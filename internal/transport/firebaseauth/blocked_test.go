package firebaseauth

import (
	"context"
	"testing"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/domain"
)

func TestBlockedTransportFailsEveryOperationClosed(t *testing.T) {
	transport := BlockedTransport{}
	errors := []error{}
	_, err := transport.SendCode(context.Background(), auth.SendCodeRequest{})
	errors = append(errors, err)
	_, err = transport.ExchangeLoginCode(context.Background(), auth.LoginCodeRequest{})
	errors = append(errors, err)
	_, err = transport.SignIn(context.Background(), auth.SignInRequest{})
	errors = append(errors, err)
	_, err = transport.Refresh(context.Background(), auth.RefreshRequest{})
	errors = append(errors, err)
	_, err = transport.LookupAccount(context.Background(), auth.LookupRequest{})
	errors = append(errors, err)
	for index, err := range errors {
		if !auth.IsErrorType(err, domain.ErrorContractProtocolChanged) {
			t.Fatalf("operation %d error = %v", index, err)
		}
	}
}
