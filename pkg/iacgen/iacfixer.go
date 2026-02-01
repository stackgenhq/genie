package iacgen

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/appcd-dev/go-lib/logger"
	"golang.org/x/sync/errgroup"
)

func NewFixer(policyChecker PolicyChecker, validator IACValidator) Fixer {
	return fixer{
		policyChecker: policyChecker,
		validator:     validator,
	}
}

type Fixer interface {
	FixIAC(ctx context.Context, req FixIACRequest) (FixIACResult, error)
}

type FixIACRequest struct {
	IACFilePath string
}

type FixIACResult struct {
	Notes []string
}

type fixer struct {
	policyChecker PolicyChecker
	validator     IACValidator
}

func (f fixer) FixIAC(ctx context.Context, req FixIACRequest) (result FixIACResult, err error) {
	notes, err := f.getIssues(ctx, req.IACFilePath)
	if err != nil {
		return FixIACResult{}, err
	}
	if len(notes) == 0 {
		return FixIACResult{}, nil
	}
	// Use an agent to fix the IAC file based on th notes
	return FixIACResult{
		Notes: notes,
	}, nil
}

func (f fixer) getIssues(ctx context.Context, iacPath string) ([]string, error) {
	notes := make([]string, 0)
	errGroup, ctx := errgroup.WithContext(ctx)
	lock := sync.Mutex{}
	errGroup.Go(func() error {
		policyNotes, err := f.checkPolicies(ctx, iacPath)
		if err != nil {
			return fmt.Errorf("error checking policies: %w", err)
		}
		lock.Lock()
		defer lock.Unlock()

		notes = append(notes, policyNotes...)
		return nil
	})
	errGroup.Go(func() error {
		tfNotes, err := f.tfValidate(ctx, iacPath)
		if err != nil {
			return fmt.Errorf("error validating the IAC file: %w", err)
		}
		lock.Lock()
		defer lock.Unlock()
		notes = append(notes, tfNotes...)
		return nil
	})
	err := errGroup.Wait()
	if err != nil {
		return nil, err
	}

	return notes, nil
}

func (f fixer) tfValidate(ctx context.Context, iacPath string) ([]string, error) {
	logr := logger.GetLogger(ctx).With("fn", "tfValidate", "iacFilePath", iacPath)
	defer func(startTime time.Time) {
		logr.Info("done with tfValidate", "time", time.Since(startTime).String())
	}(time.Now())
	validationResponse, err := f.validator.Validate(ctx, IACValidatorInput{
		IACFilePath: iacPath,
	})
	if err != nil {
		return nil, fmt.Errorf("error validating the IAC file: %w", err)
	}
	logr.Info("IAC file is valid",
		"isValid", validationResponse.IsValid,
		"filesChecked", validationResponse.FilesChecked,
	)
	return validationResponse.Notes, nil
}

func (f fixer) checkPolicies(ctx context.Context, iacPath string) ([]string, error) {
	logr := logger.GetLogger(ctx).With("fn", "checkPolicies", "iacFilePath", iacPath)
	logr.Info("checking policies", "iacFilePath", iacPath)
	defer func(startTime time.Time) {
		logr.Info("done with policy check", "time", time.Since(startTime).String())
	}(time.Now())
	validator, err := f.policyChecker.CheckPolicy(ctx, PolicyCheckRequest{
		IACSource: iacPath,
	})
	if err != nil {
		return nil, fmt.Errorf("error validating the IAC file: %w", err)
	}
	logr.Info("IAC file is valid",
		"compliant", validator.Compliant,
	)
	notes := make([]string, len(validator.Violations))
	for i, violation := range validator.Violations {
		notes[i] = violation.String()
	}
	return notes, nil
}
