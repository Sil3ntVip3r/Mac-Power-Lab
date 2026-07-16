//go:build darwin

package priority

import (
	"context"
	"os"
	"reflect"
	"testing"
)

func TestSettingCurrentNiceIsNoop(t *testing.T) {
	value, err := currentNice()
	if err != nil {
		t.Fatal(err)
	}
	if err := setCurrent(context.Background(), value); err != nil {
		t.Fatal(err)
	}
}

func TestReniceArgumentsUseAbsolutePriority(t *testing.T) {
	want := []string{"/usr/bin/renice", "0", "-p", "123"}
	if got := reniceArguments(0, 123); !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments=%v want=%v", got, want)
	}
}

func TestCurrentAndForPIDAgree(t *testing.T) {
	current, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	byPID, err := ForPID(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if current != byPID {
		t.Fatalf("current=%d byPID=%d", current, byPID)
	}
}
