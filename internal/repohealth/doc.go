// Package repohealth holds the repository-level guards that no single package
// can assert on its own, because they have to see the whole tree at once.
//
// The guards here are deliberately tests rather than linter config: a linter
// can measure a function, a file or a package, but not a policy that spans
// directories (which files are allowed to be big, and how big they may still
// be). Each guard is a ratchet — it fails on regression and on an entry that
// is no longer needed, so the exceptions list only ever shrinks.
package repohealth
