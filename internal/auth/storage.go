package auth

import (
	"bytes"
	"context"
)

// LoadActive validates both fixed slots and returns the highest valid generation.
func LoadActive(ctx context.Context, store CredentialStore) (Credential, Slot, error) {
	type candidate struct {
		credential Credential
		slot       Slot
		bytes      []byte
	}
	var valid []candidate
	present := 0
	for _, slot := range []Slot{SlotA, SlotB} {
		data, err := store.Load(ctx, slot)
		if err != nil {
			return Credential{}, "", storeError("unavailable", true)
		}
		if len(data) == 0 {
			continue
		}
		present++
		credential, err := DecodeCredential(data)
		if err != nil {
			continue
		}
		valid = append(valid, candidate{credential: credential, slot: slot, bytes: data})
	}
	if len(valid) == 0 {
		if present != 0 {
			return Credential{}, "", storeError("corrupt", false)
		}
		return Credential{}, "", requiredError()
	}
	if len(valid) == 2 && valid[0].credential.Generation == valid[1].credential.Generation {
		if !bytes.Equal(valid[0].bytes, valid[1].bytes) {
			return Credential{}, "", storeError("corrupt", false)
		}
		return valid[0].credential, valid[0].slot, nil
	}
	if len(valid) == 1 || valid[0].credential.Generation > valid[1].credential.Generation {
		return valid[0].credential, valid[0].slot, nil
	}
	return valid[1].credential, valid[1].slot, nil
}

func CommitNext(ctx context.Context, store CredentialStore, priorSlot Slot, credential Credential) (Slot, error) {
	target := SlotA
	if priorSlot == SlotA {
		target = SlotB
	}
	data, err := EncodeCredential(credential)
	if err != nil {
		return "", persistenceError(store.Backend())
	}
	if err := store.Store(ctx, target, data); err != nil {
		return "", persistenceError(store.Backend())
	}
	actual, err := store.Load(ctx, target)
	if err != nil || !bytes.Equal(actual, data) {
		_ = store.Delete(ctx, target)
		remaining, deleteErr := store.Load(ctx, target)
		if deleteErr != nil || len(remaining) != 0 {
			return "", persistenceError(store.Backend())
		}
		return "", persistenceError(store.Backend())
	}
	if _, err := DecodeCredential(actual); err != nil {
		return "", persistenceError(store.Backend())
	}
	return target, nil
}
