//go:build !darwin

package priority

import "context"

func currentNice() (int, error) { return 0, nil }

func setCurrent(context.Context, int) error { return nil }

func processNice(int) (int, error) { return 0, nil }
