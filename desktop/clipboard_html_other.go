//go:build !darwin

package main

func writeMacClipboardHTML(text, html string) error { return errClipboardHTMLUnsupported }
func readMacClipboardHTML() (string, error)         { return "", nil }
