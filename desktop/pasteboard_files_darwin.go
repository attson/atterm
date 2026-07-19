//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// atterm_pasteboard_file_paths returns a NUL-separated UTF-8 buffer of file
// URL paths currently on the general pasteboard, or NULL if there are none.
// The caller must free() the returned buffer. Length (bytes, including the
// trailing NUL after each entry) is written back through out_len.
static char *atterm_pasteboard_file_paths(int *out_len) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSDictionary *options = @{NSPasteboardURLReadingFileURLsOnlyKey: @YES};
        NSArray<Class> *classes = @[[NSURL class]];
        NSArray<NSURL *> *urls = [pb readObjectsForClasses:classes options:options];
        if (urls == nil || urls.count == 0) {
            *out_len = 0;
            return NULL;
        }

        NSMutableData *buf = [NSMutableData data];
        for (NSURL *u in urls) {
            NSString *p = [u path];
            if (p == nil || p.length == 0) continue;
            const char *cstr = [p UTF8String];
            if (cstr == NULL) continue;
            [buf appendBytes:cstr length:strlen(cstr)];
            char zero = 0;
            [buf appendBytes:&zero length:1];
        }
        int n = (int)buf.length;
        if (n == 0) {
            *out_len = 0;
            return NULL;
        }
        char *out = (char *)malloc(n);
        if (out == NULL) {
            *out_len = 0;
            return NULL;
        }
        memcpy(out, buf.bytes, n);
        *out_len = n;
        return out;
    }
}
*/
import "C"

import (
	"strings"
	"unsafe"
)

// readPasteboardFileURLs reads absolute file system paths from any file URL
// items on the macOS general pasteboard. Empty when the pasteboard has no
// file URLs (e.g. it holds only text or an image blob).
func readPasteboardFileURLs() []string {
	var n C.int
	buf := C.atterm_pasteboard_file_paths(&n)
	if buf == nil || n == 0 {
		return nil
	}
	defer C.free(unsafe.Pointer(buf))
	raw := C.GoStringN(buf, n)
	parts := strings.Split(raw, "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
