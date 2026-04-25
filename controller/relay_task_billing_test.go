package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

func TestShouldUsePerCallBilling(t *testing.T) {
	originalPatches := constant.TaskPricePatches
	t.Cleanup(func() { constant.TaskPricePatches = originalPatches })
	constant.TaskPricePatches = []string{"patched-model"}

	tests := []struct {
		name      string
		modelName string
		priceData types.PriceData
		want      bool
	}{
		{
			name:      "model price without dynamic ratios remains per call",
			modelName: "priced-model",
			priceData: types.PriceData{UsePrice: true},
			want:      true,
		},
		{
			name:      "model price with dynamic ratios settles on completion",
			modelName: "grok-imagine-video",
			priceData: types.PriceData{UsePrice: true, OtherRatios: map[string]float64{"seconds": 8}},
			want:      false,
		},
		{
			name:      "explicit task price patch remains per call",
			modelName: "patched-model",
			priceData: types.PriceData{UsePrice: true, OtherRatios: map[string]float64{"seconds": 8}},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldUsePerCallBilling(tt.modelName, tt.priceData))
		})
	}
}
