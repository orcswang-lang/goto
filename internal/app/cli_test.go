package app

import (
	"flag"
	"reflect"
	"testing"
)

func TestReorderArgs(t *testing.T) {
	newFlagSet := func() *flag.FlagSet {
		fs := flag.NewFlagSet("goto", flag.ContinueOnError)
		fs.String("jumper", "", "")
		fs.String("config", "", "")
		fs.String("host", "", "")
		fs.Bool("list", false, "")
		return fs
	}

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "flags after target",
			args: []string{"goto", "app-01", "-jumper", "prod", "-config", "/tmp/c.yaml"},
			want: []string{"goto", "-jumper", "prod", "-config", "/tmp/c.yaml", "app-01"},
		},
		{
			name: "flags before target",
			args: []string{"goto", "-jumper", "prod", "app-01"},
			want: []string{"goto", "-jumper", "prod", "app-01"},
		},
		{
			name: "equals form",
			args: []string{"goto", "app-01", "-jumper=prod"},
			want: []string{"goto", "-jumper=prod", "app-01"},
		},
		{
			name: "bool flag needs no value",
			args: []string{"goto", "app-01", "-list"},
			want: []string{"goto", "-list", "app-01"},
		},
		{
			name: "flag value consumed verbatim",
			args: []string{"goto", "-host", "-weird-host"},
			want: []string{"goto", "-host", "-weird-host"},
		},
		{
			name: "double dash terminates flags",
			args: []string{"goto", "app-01", "--", "-jumper", "prod"},
			want: []string{"goto", "app-01", "--", "-jumper", "prod"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := reorderArgs(newFlagSet(), test.args)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("reorderArgs(%v) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
