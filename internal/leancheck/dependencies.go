package leancheck

import _ "embed"

//go:embed Dependencies.lean
var dependencySource string

type DeclarationReference struct {
	Declaration string `json:"declaration"`
	Module      string `json:"module"`
	Direct      bool   `json:"direct"`
}

// References describe the elaborated proof term, not logical indispensability.
type ProofDependencies struct {
	ProcedureSHA256   string                 `json:"procedure_sha256"`
	Direct            []string               `json:"direct"`
	LibraryReferences []DeclarationReference `json:"library_references"`
}

func DependencyDigest() string { return Digest(dependencySource) }

func (d *ProofDependencies) Uses(module, declaration string) bool {
	if d == nil || d.ProcedureSHA256 != DependencyDigest() {
		return false
	}
	for _, r := range d.LibraryReferences {
		if r.Module == module && r.Declaration == declaration {
			return true
		}
	}
	return false
}
