package email

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// internalMockService implements Service for internal provider tests.
type internalMockService struct {
	SendFunc     func(ctx context.Context, req SendRequest) error
	ReadFunc     func(ctx context.Context, filter string) ([]*Email, error)
	ValidateFunc func(ctx context.Context) error
}

func (m *internalMockService) Send(ctx context.Context, req SendRequest) error {
	if m.SendFunc != nil {
		return m.SendFunc(ctx, req)
	}
	return nil
}

func (m *internalMockService) Read(ctx context.Context, filter string) ([]*Email, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx, filter)
	}
	return nil, nil
}

func (m *internalMockService) Validate(ctx context.Context) error {
	if m.ValidateFunc != nil {
		return m.ValidateFunc(ctx)
	}
	return nil
}

var _ = Describe("Provider Test", func() {
	var (
		svc *internalMockService
		p   *ToolProvider
	)

	BeforeEach(func() {
		svc = &internalMockService{}
		p = NewToolProvider(svc)
	})

	Describe("GetTools", func() {
		It("returns the correct number of tools", func() {
			tools := p.GetTools()
			Expect(tools).To(HaveLen(2))
		})
	})
})
