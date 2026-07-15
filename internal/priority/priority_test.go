package priority

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"testing"
)

func TestValidateNiceBounds(t *testing.T) {
	for _, value := range []int{MinimumNice, 0, MaximumNice} {
		if err := ValidateNice(value); err != nil {
			t.Fatalf("value %d: %v", value, err)
		}
	}
	for _, value := range []int{MinimumNice - 1, MaximumNice + 1} {
		if err := ValidateNice(value); err == nil {
			t.Fatalf("value %d unexpectedly accepted", value)
		}
	}
}

func TestNormalizedChildLaunchOrdering(t *testing.T) {
	var order []string
	set := func(_ context.Context, value int) error {
		order = append(order, "set:"+string(rune('0'+value)))
		return nil
	}
	start := func(context.Context, string, ...string) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
		order = append(order, "start")
		return nil, nil, nil, nil
	}
	if _, _, _, err := startNormalizedWith(context.Background(), 5, "workload", nil, set, start); err != nil {
		t.Fatal(err)
	}
	if want := []string{"set:0", "start", "set:5"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
}

func TestNormalizedChildRestoreFailureIsFatal(t *testing.T) {
	calls := 0
	set := func(context.Context, int) error {
		calls++
		if calls == 2 {
			return errors.New("restore failed")
		}
		return nil
	}
	start := func(context.Context, string, ...string) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
		return nil, nil, nil, nil
	}
	if _, _, _, err := startNormalizedWith(context.Background(), 10, "workload", nil, set, start); err == nil {
		t.Fatal("expected restore failure")
	}
}

func TestZeroPriorityStartsWithoutProcessTransition(t *testing.T) {
	set := func(context.Context, int) error {
		t.Fatal("zero-priority child must not change the parent")
		return nil
	}
	started := false
	start := func(context.Context, string, ...string) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
		started = true
		return nil, nil, nil, nil
	}
	if _, _, _, err := startNormalizedWith(context.Background(), 0, "workload", nil, set, start); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("child was not started")
	}
}
