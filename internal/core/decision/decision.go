package decision

import (
	"context"

	"github.com/zhouyg1992/nectar/internal/memory"
	"github.com/zhouyg1992/nectar/internal/memory/comb"
)

// DecisionService is the future Jev integration point.
type DecisionService interface {
	ShouldSurface(ctx context.Context, candidate comb.Lesson, existing []comb.Lesson) (bool, float64, error)
	SelectRelevant(ctx context.Context, prompt string, candidates []memory.Memory) ([]memory.Memory, error)
}

// Noop surfaces everything with full confidence; it is the v1 implementation.
type Noop struct{}

func (Noop) ShouldSurface(context.Context, comb.Lesson, []comb.Lesson) (bool, float64, error) {
	return true, 1.0, nil
}

func (Noop) SelectRelevant(_ context.Context, _ string, candidates []memory.Memory) ([]memory.Memory, error) {
	return candidates, nil
}

var _ DecisionService = Noop{}
