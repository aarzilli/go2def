package main

import (
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

const packageCacheEnabled = false

var globalFset *token.FileSet

func getFileSet(pos token.Pos) *token.FileSet {
	return globalFset
}

func getPosition(pos token.Pos) token.Position {
	return globalFset.Position(pos)
}

func loadPackages(out io.Writer, cwd, path string) ([]*packages.Package, error) {
	if globalFset == nil {
		globalFset = token.NewFileSet()
	}

	if !packageCacheEnabled {
		return packages.Load(&packages.Config{Mode: packages.LoadAllSyntax, Dir: cwd, Fset: globalFset}, filepath.Dir(path))
	}

	pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadImports, Dir: cwd}, filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	modifiedPkgs := map[string]struct{}{}

	for _, pkg := range pkgs {
		addModifiedPkgs(modifiedPkgs, cwd, pkg)
	}

	if len(modifiedPkgs) > 0 {
		if verbose {
			for mpkg := range modifiedPkgs {
				log.Printf("reloading %q", mpkg)
			}
		}

		modifiedPkgsList := make([]string, 0, len(modifiedPkgs))
		for k := range modifiedPkgs {
			modifiedPkgsList = append(modifiedPkgsList, k)
		}

		reloadedPkgs, err := packages.Load(&packages.Config{Mode: packages.LoadSyntax, Dir: cwd, Fset: globalFset}, modifiedPkgsList...)
		if err != nil {
			return nil, err
		}

		for _, pkg := range reloadedPkgs {
			updateCache(cwd, pkg)
		}
	}

	return []*packages.Package{packageCache[makePackageCacheKey(cwd, pkgs[0])].Package}, nil
}

func addModifiedPkgs(modifiedPkgs map[string]struct{}, cwd string, pkg *packages.Package) bool {
	_, modified := modifiedPkgs[pkg.PkgPath]
	if modified {
		return true
	}
	if modifiedPkg(cwd, pkg) {
		modified = true
		modifiedPkgs[pkg.PkgPath] = struct{}{}
	}

	for _, child := range pkg.Imports {
		if addModifiedPkgs(modifiedPkgs, cwd, child) {
			modified = true
		}
	}
	if modified {
		modifiedPkgs[pkg.PkgPath] = struct{}{}
	}
	return modified
}

type packageCacheKey struct {
	Name string
	Cwd  string
}

var packageCache = map[packageCacheKey]*cachedPackage{}

type cachedPackage struct {
	Package *packages.Package
	Files   []cachedFile
}

type cachedFile struct {
	path         string
	lastModified int64
}

func makePackageCacheKey(cwd string, pkg *packages.Package) packageCacheKey {
	return packageCacheKey{pkg.PkgPath, cwd}
}

func updateCache(cwd string, pkg *packages.Package) {
	key := makePackageCacheKey(cwd, pkg)
	entry, ok := packageCache[key]
	if ok {
		entry.Package.GoFiles = pkg.GoFiles
		entry.Package.CompiledGoFiles = pkg.GoFiles
		entry.Package.OtherFiles = pkg.OtherFiles
		entry.Package.ExportFile = pkg.ExportFile
		entry.Package.Types = pkg.Types
		entry.Package.Fset = pkg.Fset
		entry.Package.IllTyped = pkg.IllTyped
		entry.Package.Syntax = pkg.Syntax
		entry.Package.TypesInfo = pkg.TypesInfo
	} else {
		entry = &cachedPackage{Package: pkg, Files: []cachedFile{}}
		packageCache[key] = entry
	}

	entry.Files = entry.Files[:0]

	for _, path := range entry.Package.GoFiles {
		fi, err := os.Stat(path)
		if err == nil {
			entry.Files = append(entry.Files, cachedFile{path: path, lastModified: fi.ModTime().Unix()})
		}
		//TODO: compute last modified
	}
}

func modifiedPkg(cwd string, pkg *packages.Package) bool {
	entry := packageCache[makePackageCacheKey(cwd, pkg)]
	if entry == nil {
		return true
	}
	for _, f := range entry.Files {
		fi, err := os.Stat(f.path)
		if err != nil {
			return true
		}
		if fi.ModTime().Unix() != f.lastModified {
			return true
		}
	}
	return false
}
