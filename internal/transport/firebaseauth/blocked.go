package firebaseauth

import (
	"context"

	"github.com/KalebCole/partiful/internal/auth"
)

// BlockedTransport performs no I/O while authentication evidence gates remain open.
type BlockedTransport struct{}

func blocked() error {
	return auth.ProtocolChangedError()
}
func (BlockedTransport) SendCode(context.Context, auth.SendCodeRequest) (auth.SendCodeResult, error) {
	return auth.SendCodeResult{}, blocked()
}
func (BlockedTransport) ExchangeLoginCode(context.Context, auth.LoginCodeRequest) (auth.LoginCodeResult, error) {
	return auth.LoginCodeResult{}, blocked()
}
func (BlockedTransport) SignIn(context.Context, auth.SignInRequest) (auth.Session, error) {
	return auth.Session{}, blocked()
}
func (BlockedTransport) Refresh(context.Context, auth.RefreshRequest) (auth.Session, error) {
	return auth.Session{}, blocked()
}
func (BlockedTransport) LookupAccount(context.Context, auth.LookupRequest) (auth.Account, error) {
	return auth.Account{}, blocked()
}

var _ auth.AuthTransport = BlockedTransport{}
