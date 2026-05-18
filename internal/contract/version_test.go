package contract

import (
	"errors"
	"testing"
)

func TestCheckEnvelopeVersion(t *testing.T) {
	ok := sampleRequest()
	if err := CheckEnvelopeVersion(&ok); err != nil {
		t.Fatalf("v1 rejected: %v", err)
	}
	bad := sampleRequest()
	bad.Version = 2
	if err := CheckEnvelopeVersion(&bad); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("v2: err = %v, want ErrUnsupportedVersion (no downgrade guess)", err)
	}
}

func TestNegotiateMPCProtocol(t *testing.T) {
	cases := []struct {
		name          string
		local, remote []string
		want          string
		wantErr       bool
	}{
		{
			name:   "highest common",
			local:  []string{"/tss/mpc/1.0.0", "/tss/mpc/1.2.0", "/tss/mpc/2.0.0"},
			remote: []string{"/tss/mpc/1.2.0", "/tss/mpc/1.0.0"},
			want:   "/tss/mpc/1.2.0",
		},
		{
			name:   "ignores non-mpc and unparsable",
			local:  []string{"/tss/heartbeat/1.0.0", "/tss/mpc/1.0.0", "/tss/mpc/bad"},
			remote: []string{"/tss/mpc/1.0.0", "/tss/heartbeat/1.0.0"},
			want:   "/tss/mpc/1.0.0",
		},
		{
			name:    "no common version",
			local:   []string{"/tss/mpc/1.0.0"},
			remote:  []string{"/tss/mpc/2.0.0"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NegotiateMPCProtocol(c.local, c.remote)
			if c.wantErr {
				if !errors.Is(err, ErrNoCommonProtocol) {
					t.Fatalf("err = %v, want ErrNoCommonProtocol", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
