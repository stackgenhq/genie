package iacgen

import "context"

type IACWriter interface {
	WriteIAC(ctx context.Context, req IACWriterInput) (IACWriterResult, error)
}

type IACWriterInput struct {
	AnalysisNDJSONFilePath string
	SaveTo                 string
}

type IACWriterResult struct {
	IACFilePath string
	Notes       []string
}

func NewIACWriter() IACWriter {
	return iacWriter{}
}

type iacWriter struct{}

func (i iacWriter) WriteIAC(ctx context.Context, req IACWriterInput) (IACWriterResult, error) {
	return IACWriterResult{}, nil
}
