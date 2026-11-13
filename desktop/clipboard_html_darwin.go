//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

static int atterm_write_clipboard_html(const char *text, int text_len, const char *html, int html_len) {
    @autoreleasepool {
        NSString *plain = [[NSString alloc] initWithBytes:text length:text_len encoding:NSUTF8StringEncoding];
        NSString *rich = [[NSString alloc] initWithBytes:html length:html_len encoding:NSUTF8StringEncoding];
        if (plain == nil || rich == nil) return 0;
        NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
        if (![item setString:plain forType:NSPasteboardTypeString] ||
            ![item setString:rich forType:NSPasteboardTypeHTML]) return 0;
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        return [pb writeObjects:@[item]] ? 1 : 0;
    }
}

static char *atterm_read_clipboard_html(int max_len, int *out_len) {
    @autoreleasepool {
        NSData *data = [[NSPasteboard generalPasteboard] dataForType:NSPasteboardTypeHTML];
        *out_len = 0;
        if (data == nil || data.length == 0) return NULL;
        if (data.length > (NSUInteger)max_len) { *out_len = -1; return NULL; }
        char *out = (char *)malloc(data.length);
        if (out == NULL) { *out_len = -2; return NULL; }
        memcpy(out, data.bytes, data.length);
        *out_len = (int)data.length;
        return out;
    }
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func writeMacClipboardHTML(text, html string) error {
	plain, rich := C.CString(text), C.CString(html)
	defer C.free(unsafe.Pointer(plain))
	defer C.free(unsafe.Pointer(rich))
	if C.atterm_write_clipboard_html(plain, C.int(len(text)), rich, C.int(len(html))) == 0 {
		return errors.New("write rich clipboard failed")
	}
	return nil
}

func readMacClipboardHTML() (string, error) {
	var n C.int
	buf := C.atterm_read_clipboard_html(C.int(maxClipboardHTMLBytes), &n)
	if n == -1 {
		return "", errClipboardHTMLTooLarge
	}
	if n == -2 {
		return "", errors.New("read rich clipboard failed")
	}
	if buf == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(buf))
	return C.GoStringN(buf, n), nil
}
