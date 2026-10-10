// This file exists only to put web/ outside the root Go module's boundary.
// web/node_modules/flatted/golang/pkg/flatted ships a .go file with no go.mod
// of its own, so without this, `go build/vet/test ./...` from the repo root
// would pick it up - and silently change the Go build graph on every
// `npm install` that bumps a transitive JS dependency.
module criaisis/web/_notgo

go 1.27.0
