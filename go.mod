module github.com/septagon-oss/pk-core

go 1.26

require (
	github.com/septagon-oss/pk-shared v0.5.0
	github.com/septagon-oss/problem v0.1.1
	golang.org/x/crypto v0.54.0
)

require golang.org/x/sys v0.47.0 // indirect

retract v0.0.0 // broken: contained local replace directives
