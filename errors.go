package juicer

import "errors"

// ErrJuiceFails is wrapped by the error Inline returns where juice itself
// fails on the document and this package reproduces the failure: in a
// compatibility mode, and for pseudo-element content juice cannot read. A
// caller can tell it apart from any other failure with errors.Is.
var ErrJuiceFails = errors.New("juice fails on this document")
