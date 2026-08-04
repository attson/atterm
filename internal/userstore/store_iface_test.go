package userstore

// Compile-time assertion that *DBStore implements Store. This file
// has no runtime tests; if the assignment fails to compile, the contract
// is broken.
var _ Store = (*DBStore)(nil)
