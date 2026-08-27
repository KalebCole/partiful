package credentialstore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"

	"github.com/KalebCole/partiful/internal/auth"
)

const fileBackend auth.BackendKind = "protected-file"

type FileStore struct{ root string }

func NewFileStore(root string) *FileStore          { return &FileStore{root: filepath.Clean(root)} }
func (store *FileStore) Backend() auth.BackendKind { return fileBackend }

func DataRoot(goos, home, _ string, _ string) string {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "partiful")
	case "windows":
		return filepath.Join(home, "AppData", "Local", "Partiful")
	default:
		return filepath.Join(home, ".local", "share", "partiful")
	}
}

func DefaultDataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("cannot locate user data root")
	}
	return DataRoot(runtime.GOOS, home, os.Getenv("XDG_DATA_HOME"), os.Getenv("LOCALAPPDATA")), nil
}

func slotName(slot auth.Slot) (string, error) {
	switch slot {
	case auth.SlotA:
		return "slot-a.json", nil
	case auth.SlotB:
		return "slot-b.json", nil
	default:
		return "", errors.New("invalid credential slot")
	}
}

func (store *FileStore) directory(create bool) (string, error) {
	root, err := filepath.Abs(store.root)
	if err != nil {
		return "", errors.New("invalid credential root")
	}
	if create {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", errors.New("credential root unavailable")
		}
	}
	if err := requireRealDirectory(root, 0o700); err != nil {
		if !create && errors.Is(err, os.ErrNotExist) {
			return filepath.Join(root, "credentials"), nil
		}
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("credential root unavailable")
	}
	root = filepath.Clean(resolved)
	directory := filepath.Join(root, "credentials")
	if create {
		if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", errors.New("credential directory unavailable")
		}
	}
	if err := requireRealDirectory(directory, 0o700); err != nil {
		if !create && errors.Is(err, os.ErrNotExist) {
			return directory, nil
		}
		return "", err
	}
	return directory, nil
}

func requireRealDirectory(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("credential path is not a real directory")
	}
	directory, err := os.Open(path)
	if err != nil {
		return errors.New("credential directory unavailable")
	}
	defer directory.Close()
	opened, err := directory.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("credential directory changed during validation")
	}
	if runtime.GOOS != "windows" {
		if err := directory.Chmod(mode); err != nil {
			return errors.New("credential directory permissions unavailable")
		}
		info, err = os.Lstat(path)
		if err != nil || !os.SameFile(info, opened) || info.Mode().Perm()&0o077 != 0 {
			return errors.New("credential directory permissions are too broad")
		}
	}
	return nil
}

func credentialFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || linkCount(info) != 1 {
		return nil, errors.New("credential file is not private")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return nil, errors.New("credential file permissions are unsafe")
	}
	return info, nil
}

func linkCount(info os.FileInfo) uint64 {
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.IsValid() {
		field := value.FieldByName("Nlink")
		if field.IsValid() {
			return field.Convert(reflect.TypeOf(uint64(0))).Uint()
		}
	}
	return 1
}

func (store *FileStore) path(slot auth.Slot, create bool) (string, error) {
	name, err := slotName(slot)
	if err != nil {
		return "", err
	}
	directory, err := store.directory(create)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

func (store *FileStore) Load(ctx context.Context, slot auth.Slot) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := store.path(slot, false)
	if err != nil {
		return nil, err
	}
	expected, err := credentialFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("credential file unavailable")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(expected, opened) {
		return nil, errors.New("credential file changed during validation")
	}
	value, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return nil, errors.New("credential file unavailable")
	}
	return value, nil
}

func (store *FileStore) Store(ctx context.Context, slot auth.Slot, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := store.path(slot, true)
	if err != nil {
		return err
	}
	if _, err := credentialFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".credential-*")
	if err != nil {
		return errors.New("credential staging unavailable")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	fail := func(cause error) error { temporary.Close(); return cause }
	if err := temporary.Chmod(0o600); err != nil {
		return fail(errors.New("credential staging unavailable"))
	}
	if _, err := temporary.Write(value); err != nil {
		return fail(errors.New("credential staging unavailable"))
	}
	if err := temporary.Sync(); err != nil {
		return fail(errors.New("credential staging unavailable"))
	}
	if err := temporary.Close(); err != nil {
		return errors.New("credential staging unavailable")
	}
	if _, err := credentialFile(temporaryPath); err != nil {
		return err
	}
	if _, err := store.path(slot, true); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("credential activation unavailable")
	}
	if err := syncDirectory(directory); err != nil {
		return errors.New("credential activation unavailable")
	}
	return nil
}

func (store *FileStore) Delete(ctx context.Context, slot auth.Slot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := store.path(slot, false)
	if err != nil {
		return err
	}
	if _, err := credentialFile(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return errors.New("credential deletion unavailable")
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

var _ auth.CredentialStore = (*FileStore)(nil)
