package credentials

import (
	"errors"
	"testing"
)

func TestOSAccountMapping(t *testing.T) {
	var calls []struct{ service, account string }
	o := &OS{readFn: func(service, account string) (string, error) {
		calls = append(calls, struct{ service, account string }{service, account})
		switch account {
		case "prod":
			return "hunter2", nil
		case "prod:totp":
			return "JBSWY3DPEHPK3PXP", nil
		default:
			return "", ErrNotFound
		}
	}}

	pw, err := o.GetPassword("prod")
	if err != nil {
		t.Fatal(err)
	}
	if pw != "hunter2" {
		t.Fatalf("GetPassword = %q, want hunter2", pw)
	}

	secret, err := o.GetTOTPSecret("prod")
	if err != nil {
		t.Fatal(err)
	}
	if secret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("GetTOTPSecret = %q, want JBSWY3DPEHPK3PXP", secret)
	}

	if len(calls) != 2 {
		t.Fatalf("calls = %+v, want 2", calls)
	}
	if calls[0].service != "goto" || calls[0].account != "prod" {
		t.Fatalf("password call = %+v, want goto/prod", calls[0])
	}
	if calls[1].account != "prod:totp" {
		t.Fatalf("totp call = %+v, want account prod:totp", calls[1])
	}
}

func TestOSMissingCredential(t *testing.T) {
	o := &OS{readFn: func(service, account string) (string, error) {
		return "", ErrNotFound
	}}
	if _, err := o.GetPassword("prod"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPassword err = %v, want ErrNotFound", err)
	}
	if _, err := o.GetTOTPSecret("prod"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetTOTPSecret err = %v, want ErrNotFound", err)
	}
}
