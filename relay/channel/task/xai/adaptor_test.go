package xai

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testContextWithTaskRequest(req relaycommon.TaskSubmitReq) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", req)
	return ctx
}

func TestEstimateBilling_TextToVideoDurationResolutionAndInputImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt:   "make a product video",
		Duration: 10,
		Metadata: map[string]interface{}{
			"resolution":             "720p",
			"reference_image_urls": []interface{}{"https://example.com/a.png", "https://example.com/b.png"},
		},
	}
	info := &relaycommon.RelayInfo{Action: "textGenerate"}

	ratios := adaptor.EstimateBilling(testContextWithTaskRequest(req), info)

	require.NotNil(t, ratios)
	assert.Equal(t, 10.0, ratios[ratioKeySeconds])
	assert.InDelta(t, 1.4, ratios[ratioKeyResolution], 0.000001)
	// Two input images cost $0.04. Output cost is $0.50 * 10s * 1.4 = $7.00.
	assert.InDelta(t, 1.005714, ratios[ratioKeyInputAdjustment], 0.000001)
}

func TestEstimateBilling_ExtendVideoIncludesInputVideoSeconds(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt:   "extend the scene",
		Duration: 6,
		Metadata: map[string]interface{}{
			"mode":                "extend-video",
			"video_url":           "https://example.com/source.mp4",
			"input_video_seconds": 12,
		},
	}
	info := &relaycommon.RelayInfo{Action: "remixGenerate"}

	ratios := adaptor.EstimateBilling(testContextWithTaskRequest(req), info)

	require.NotNil(t, ratios)
	assert.Equal(t, 6.0, ratios[ratioKeySeconds])
	assert.Equal(t, 1.0, ratios[ratioKeyResolution])
	// Input video costs $1.20. Output cost is $0.50 * 6s = $3.00.
	assert.InDelta(t, 1.4, ratios[ratioKeyInputAdjustment], 0.000001)
}

func TestParseTaskResultDoneUsesActualDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/video.mp4","duration":8.2},"model":"grok-imagine-video"}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, string(model.TaskStatusSuccess), info.Status)
	assert.Equal(t, "https://vidgen.x.ai/video.mp4", info.Url)
	assert.Equal(t, 9, info.TotalTokens)
}

func TestAdjustBillingOnCompleteUsesActualDurationAndInputAdjustment(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		Quota: 6000,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				ModelPrice: 0.5,
				GroupRatio: 2,
				OtherRatios: map[string]float64{
					ratioKeySeconds:         8,
					ratioKeyResolution:      1.4,
					ratioKeyInputAdjustment: 1.1,
				},
			},
		},
	}
	result := &relaycommon.TaskInfo{TotalTokens: 10}

	quota := adaptor.AdjustBillingOnComplete(task, result)

	// Estimated input cost: $0.50 * 8s * 1.4 * (1.1 - 1) = $0.56.
	// Actual output cost: $0.50 * 10s * 1.4 = $7.00.
	// Total at group ratio 2: ($7.56 * 2) * QuotaPerUnit.
	expected := int(7.56 * 2 * common.QuotaPerUnit)
	assert.Equal(t, expected, quota)
}

func TestBuildVideoRequestForEditVideoUsesVideoPayload(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Prompt: "make it cinematic",
		Metadata: map[string]interface{}{
			"mode":      "edit-video",
			"video_url": "https://example.com/input.mp4",
		},
	}
	info := &relaycommon.RelayInfo{UpstreamModelName: "grok-imagine-video", Action: "generate"}

	body := buildVideoRequest(req, info)

	assert.Equal(t, "grok-imagine-video", body.Model)
	require.NotNil(t, body.Video)
	assert.Equal(t, "https://example.com/input.mp4", body.Video.URL)
	assert.Nil(t, body.Image)
}
