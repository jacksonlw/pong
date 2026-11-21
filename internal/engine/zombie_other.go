//go:build !darwin && !linux

package engine

func zombie(int) bool { return false }
