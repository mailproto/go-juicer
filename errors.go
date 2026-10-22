package juicer

import "errors"

// ErrJuiceFails is wrapped by the error Inline returns, in a compatibility
// mode, where juice itself fails on the document. A caller can tell it apart
// from any other failure with errors.Is.
var ErrJuiceFails = errors.New("juice fails on this document")

// ErrInvalidOption is wrapped by the error New returns for options that
// cannot be honoured.
var ErrInvalidOption = errors.New("invalid option")

// ErrLimitExceeded is wrapped by the error Inline returns for a document over
// MaxInputSize or MaxDepth.
var ErrLimitExceeded = errors.New("limit exceeded")
