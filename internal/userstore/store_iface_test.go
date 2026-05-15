package userstore

// Compile-time assertion that *SQLiteStore implements Store. This file
// has no runtime tests; if the assignment fails to compile, the contract
// is broken.
var _ Store = (*SQLiteStore)(nil)
