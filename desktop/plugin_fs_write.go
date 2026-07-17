package main

// PluginFS write-side bindings — kept in a separate file for readability.
// Every method delegates to fsAccess, which runs the path through resolve()
// (allowRoots + deny) before touching disk. The Wails runtime auto-binds
// every exported method on PluginFS and generates wailsjs entrypoints for
// the frontend.

// WriteFile writes data to path atomically (tmp + rename). expectedModTime=0
// disables the CAS check; createIfMissing=true allows creating a new file.
func (p *PluginFS) WriteFile(path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error) {
	return p.access.writeFile(path, data, expectedModTime, createIfMissing)
}

// CreateFile creates path as an empty file. Fails if the path already exists.
func (p *PluginFS) CreateFile(path string) (FileMetaInfo, error) {
	return p.access.createFile(path)
}

// Rename moves from → to. Both endpoints go through resolve().
func (p *PluginFS) Rename(from, to string) (FileMetaInfo, error) {
	return p.access.renamePath(from, to)
}

// Remove deletes path. recursive=false fails on non-empty directories.
func (p *PluginFS) Remove(path string, recursive bool) error {
	return p.access.removePath(path, recursive)
}

// Mkdir creates a single directory. Parent must already exist.
func (p *PluginFS) Mkdir(path string) (FileMetaInfo, error) {
	return p.access.mkdir(path)
}

// Trash sends path to the OS trash. Returns an ErrUnavailable-wrapping error
// when no supported trash command exists so the UI can offer a hard-delete
// fallback.
func (p *PluginFS) Trash(path string) error {
	return p.access.trashPath(path)
}
