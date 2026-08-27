package credentialstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"runtime"

	"github.com/KalebCole/partiful/internal/auth"
)

type CommandDriver struct{ goos, executable string }

func DefaultCommandDriver() (*CommandDriver, error) {
	name := map[string]string{"darwin": "security", "linux": "secret-tool", "windows": "powershell.exe"}[runtime.GOOS]
	if name == "" {
		return nil, errors.New("platform credential store unavailable")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, errors.New("platform credential store unavailable")
	}
	return &CommandDriver{goos: runtime.GOOS, executable: path}, nil
}
func (driver *CommandDriver) slot(slot auth.Slot) (string, error) {
	if slot == auth.SlotA {
		return "slot-a", nil
	}
	if slot == auth.SlotB {
		return "slot-b", nil
	}
	return "", errors.New("invalid credential slot")
}
func (driver *CommandDriver) command(ctx context.Context, input []byte, missingOK bool, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, driver.executable, arguments...)
	command.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if missingOK {
			if exit, ok := err.(*exec.ExitError); ok && ((driver.goos == "darwin" && exit.ExitCode() == 44) || (driver.goos == "linux" && exit.ExitCode() == 1)) {
				return nil, nil
			}
		}
		return nil, errors.New("platform credential operation failed")
	}
	return bytes.TrimSpace(output.Bytes()), nil
}
func (driver *CommandDriver) Load(ctx context.Context, slot auth.Slot) ([]byte, error) {
	name, err := driver.slot(slot)
	if err != nil {
		return nil, err
	}
	switch driver.goos {
	case "darwin":
		return driver.command(ctx, nil, true, "find-generic-password", "-s", "com.kalebcole.partiful", "-a", name, "-w")
	case "linux":
		return driver.command(ctx, nil, true, "lookup", "service", "partiful", "slot", name)
	case "windows":
		return driver.windows(ctx, "load", name, nil)
	default:
		return nil, errors.New("platform credential store unavailable")
	}
}
func (driver *CommandDriver) Store(ctx context.Context, slot auth.Slot, value []byte) error {
	name, err := driver.slot(slot)
	if err != nil {
		return err
	}
	switch driver.goos {
	case "darwin":
		_, err = driver.command(ctx, append(append([]byte(nil), value...), '\n'), false, "add-generic-password", "-U", "-s", "com.kalebcole.partiful", "-a", name, "-w")
	case "linux":
		_, err = driver.command(ctx, value, false, "store", "--label=Partiful", "service", "partiful", "slot", name)
	case "windows":
		_, err = driver.windows(ctx, "store", name, value)
	default:
		err = errors.New("platform credential store unavailable")
	}
	return err
}
func (driver *CommandDriver) Delete(ctx context.Context, slot auth.Slot) error {
	name, err := driver.slot(slot)
	if err != nil {
		return err
	}
	switch driver.goos {
	case "darwin":
		_, err = driver.command(ctx, nil, true, "delete-generic-password", "-s", "com.kalebcole.partiful", "-a", name)
	case "linux":
		_, err = driver.command(ctx, nil, true, "clear", "service", "partiful", "slot", name)
	case "windows":
		_, err = driver.windows(ctx, "delete", name, nil)
	default:
		err = errors.New("platform credential store unavailable")
	}
	return err
}

const windowsVaultScript = `$request=[Console]::In.ReadToEnd()|ConvertFrom-Json
$null=[Windows.Security.Credentials.PasswordVault,Windows.Security.Credentials,ContentType=WindowsRuntime]
$null=[Windows.Security.Credentials.PasswordCredential,Windows.Security.Credentials,ContentType=WindowsRuntime]
$vault=[Windows.Security.Credentials.PasswordVault]::new()
$resource='partiful/'+$request.slot
try{$existing=@($vault.FindAllByResource($resource))}catch{$existing=@()}
if($request.action -eq 'load'){if($existing.Count -eq 0){exit 0};$item=$existing[0];$item.RetrievePassword();[Console]::Out.Write($item.Password);exit 0}
foreach($item in $existing){$vault.Remove($item)}
if($request.action -eq 'store'){$vault.Add([Windows.Security.Credentials.PasswordCredential]::new($resource,'partiful',$request.value))}`

func (driver *CommandDriver) windows(ctx context.Context, action, slot string, value []byte) ([]byte, error) {
	payload, err := encodeWindowsRequest(action, slot, value)
	if err != nil {
		return nil, errors.New("platform credential operation failed")
	}
	return driver.command(ctx, payload, false, "-NoProfile", "-NonInteractive", "-Command", windowsVaultScript)
}

func encodeWindowsRequest(action, slot string, value []byte) ([]byte, error) {
	return json.Marshal(struct {
		Action string `json:"action"`
		Slot   string `json:"slot"`
		Value  string `json:"value"`
	}{Action: action, Slot: slot, Value: string(value)})
}

var _ OSDriver = (*CommandDriver)(nil)
