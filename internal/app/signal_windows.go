//go:build windows

package app

func signalNotifyWindowChange(ch chan<- struct{}) {}
