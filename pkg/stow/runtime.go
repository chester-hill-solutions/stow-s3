package stow

import "github.com/chester-hill-solutions/stow-s3/internal/embedded"

type (
	Runtime = embedded.Runtime
)

func Open(options Options) (*Runtime, error) { return embedded.Open(options) }
