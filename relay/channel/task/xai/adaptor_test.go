package xai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
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

func testContextWithJSONTaskRequest(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	body, err := common.Marshal(req)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx
}

func TestEstimateBilling_TextToVideoDurationResolutionAndInputImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt:   "make a product video",
		Duration: 10,
		Metadata: map[string]interface{}{
			"resolution":           "720p",
			"reference_image_urls": []interface{}{"https://example.com/a.png", "https://example.com/b.png"},
		},
	}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "textGenerate"}}

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
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "remixGenerate"}}

	ratios := adaptor.EstimateBilling(testContextWithTaskRequest(req), info)

	require.NotNil(t, ratios)
	assert.Equal(t, 6.0, ratios[ratioKeySeconds])
	assert.Equal(t, 1.0, ratios[ratioKeyResolution])
	// Input video costs $1.20. Output cost is $0.50 * 6s = $3.00.
	assert.InDelta(t, 1.4, ratios[ratioKeyInputAdjustment], 0.000001)
}

func TestEstimateBilling_EditVideoKeepsOutputAndInputSecondsSeparate(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt:   "edit the video",
		Duration: 8,
		Metadata: map[string]interface{}{
			"mode":                "edit-video",
			"video_url":           "https://example.com/input.mp4",
			"input_video_seconds": 30,
		},
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"},
		PriceData:     types.PriceData{ModelPrice: 0.5},
	}

	ratios := adaptor.EstimateBilling(testContextWithTaskRequest(req), info)

	require.NotNil(t, ratios)
	assert.Equal(t, 8.0, ratios[ratioKeySeconds])
	assert.Equal(t, 1.0, ratios[ratioKeyResolution])
	// Input video costs $3.00. Output cost is $0.50 * 8s = $4.00.
	assert.InDelta(t, 1.75, ratios[ratioKeyInputAdjustment], 0.000001)
}

func TestEstimateBilling_CustomModelPriceDoesNotScaleInputCost(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt:   "make a product video",
		Duration: 10,
		Metadata: map[string]interface{}{
			"image_url": "https://example.com/a.png",
		},
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "textGenerate"},
		PriceData:     types.PriceData{ModelPrice: 1.0},
	}

	ratios := adaptor.EstimateBilling(testContextWithTaskRequest(req), info)

	require.NotNil(t, ratios)
	// Input image costs $0.02. Output cost is custom $1.00 * 10s = $10.00.
	assert.InDelta(t, 1.002, ratios[ratioKeyInputAdjustment], 0.000001)
}

// 等价类划分：duration 可来自 duration、seconds、metadata.duration，缺省走默认 8 秒。
func TestResolveOutputDuration_EquivalenceClasses(t *testing.T) {
	tests := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want int
	}{
		{
			name: "duration field",
			req:  relaycommon.TaskSubmitReq{Duration: 12},
			want: 12,
		},
		{
			name: "seconds string",
			req:  relaycommon.TaskSubmitReq{Seconds: "9"},
			want: 9,
		},
		{
			name: "metadata duration float rounds up",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"duration": 7.2,
			}},
			want: 8,
		},
		{
			name: "default duration",
			req:  relaycommon.TaskSubmitReq{},
			want: defaultDurationSeconds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveOutputDuration(tt.req))
		})
	}
}

// 边界值分析：无效/零/负时长不能污染计费估算，应回落默认值。
func TestResolveOutputDuration_BoundaryValues(t *testing.T) {
	tests := []relaycommon.TaskSubmitReq{
		{Duration: 0, Seconds: "0", Metadata: map[string]interface{}{"duration": 0}},
		{Duration: -1, Seconds: "-2", Metadata: map[string]interface{}{"duration": -3}},
		{Seconds: "not-a-number"},
	}

	for _, req := range tests {
		assert.Equal(t, defaultDurationSeconds, resolveOutputDuration(req))
	}
}

// 判定表法：action/mode 组合应路由到官方对应 endpoint。
func TestBuildRequestURL_DecisionTable(t *testing.T) {
	tests := []struct {
		name   string
		mode   string
		action string
		want   string
	}{
		{
			name:   "text generation",
			action: constant.TaskActionTextGenerate,
			want:   "https://api.x.ai/v1/videos/generations",
		},
		{
			name:   "image generation",
			action: constant.TaskActionGenerate,
			want:   "https://api.x.ai/v1/videos/generations",
		},
		{
			name:   "edit mode",
			mode:   "edit-video",
			action: constant.TaskActionGenerate,
			want:   "https://api.x.ai/v1/videos/edits",
		},
		{
			name:   "extend action",
			action: constant.TaskActionRemix,
			want:   "https://api.x.ai/v1/videos/extensions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.x.ai"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: tt.action}}
			adaptor.Init(info)
			adaptor.mode = tt.mode

			got, err := adaptor.BuildRequestURL(info)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// 正交法：覆盖生成请求中 duration/resolution/aspect/image 输入的代表性组合。
func TestBuildVideoRequest_GenerationOrthogonalCases(t *testing.T) {
	tests := []struct {
		name                string
		req                 relaycommon.TaskSubmitReq
		wantDuration        int
		wantResolution      string
		wantAspectRatio     string
		wantImage           bool
		wantReferenceImages int
	}{
		{
			name: "duration plus 720p plus metadata image",
			req: relaycommon.TaskSubmitReq{
				Prompt:   "generate",
				Duration: 5,
				Metadata: map[string]interface{}{
					"resolution": "720p",
					"image_url":  "https://example.com/image.png",
				},
			},
			wantDuration:   5,
			wantResolution: hdResolution,
			wantImage:      true,
		},
		{
			name: "seconds plus size aspect plus multiple images",
			req: relaycommon.TaskSubmitReq{
				Prompt:  "generate",
				Seconds: "6",
				Size:    "1280x720",
				Images:  []string{"https://example.com/a.png", "https://example.com/b.png"},
			},
			wantDuration:        6,
			wantAspectRatio:     "16:9",
			wantReferenceImages: 2,
		},
		{
			name: "metadata duration plus explicit aspect plus reference metadata",
			req: relaycommon.TaskSubmitReq{
				Prompt: "generate",
				Metadata: map[string]interface{}{
					"duration":             4,
					"aspect_ratio":         "9:16",
					"reference_image_urls": []interface{}{"https://example.com/ref.png"},
				},
			},
			wantDuration:        4,
			wantAspectRatio:     "9:16",
			wantReferenceImages: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := buildVideoRequest(tt.req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-imagine-video"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionTextGenerate}})

			assert.Equal(t, tt.wantDuration, body.Duration)
			assert.Equal(t, tt.wantResolution, body.Resolution)
			assert.Equal(t, tt.wantAspectRatio, body.AspectRatio)
			assert.Equal(t, tt.wantImage, body.Image != nil)
			assert.Len(t, body.ReferenceImages, tt.wantReferenceImages)
			assert.Empty(t, body.VideoURL)
			assert.Nil(t, body.Video)
		})
	}
}

// 因果图法：video 输入与 image 输入互斥，单独出现时合法，同时出现时本地拒绝。
func TestValidateRequest_VideoImageCauseEffect(t *testing.T) {
	tests := []struct {
		name      string
		req       relaycommon.TaskSubmitReq
		wantError bool
	}{
		{
			name: "video only",
			req: relaycommon.TaskSubmitReq{Prompt: "edit", Metadata: map[string]interface{}{
				"video_url": "https://example.com/input.mp4",
			}},
		},
		{
			name: "image only",
			req:  relaycommon.TaskSubmitReq{Prompt: "generate", Image: "https://example.com/image.png"},
		},
		{
			name: "video and image conflict",
			req: relaycommon.TaskSubmitReq{Prompt: "conflict", Image: "https://example.com/image.png", Metadata: map[string]interface{}{
				"video_url": "https://example.com/input.mp4",
			}},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			ctx := testContextWithJSONTaskRequest(t, tt.req)
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

			taskErr := adaptor.ValidateRequestAndSetAction(ctx, info)

			if tt.wantError {
				require.NotNil(t, taskErr)
				assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			} else {
				require.Nil(t, taskErr)
			}
		})
	}
}

// 状态迁移法：上游状态应稳定映射到 NewAPI 任务状态。
func TestParseTaskResult_StateTransitionClasses(t *testing.T) {
	adaptor := &TaskAdaptor{}
	tests := []struct {
		name       string
		body       []byte
		wantStatus model.TaskStatus
		wantErr    bool
	}{
		{name: "queued", body: []byte(`{"status":"queued"}`), wantStatus: model.TaskStatusQueued},
		{name: "processing", body: []byte(`{"status":"processing"}`), wantStatus: model.TaskStatusInProgress},
		{name: "success", body: []byte(`{"status":"success","video":{"url":"https://example.com/out.mp4","duration":3}}`), wantStatus: model.TaskStatusSuccess},
		{name: "failed", body: []byte(`{"status":"failed","error":{"message":"blocked"}}`), wantStatus: model.TaskStatusFailure},
		{name: "unknown", body: []byte(`{"status":"mystery"}`), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := adaptor.ParseTaskResult(tt.body)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, string(tt.wantStatus), info.Status)
		})
	}
}

// 错误推测法：无效提交响应、错误响应、缺 request_id 都应转成本地可理解错误。
func TestDoResponse_ErrorGuessing(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{not-json`},
		{name: "upstream error", body: `{"error":{"message":"bad request"}}`},
		{name: "missing request id", body: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(tt.body))}

			taskID, taskData, taskErr := adaptor.DoResponse(ctx, resp, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_123"}})

			assert.Empty(t, taskID)
			assert.NotNil(t, taskErr)
			assert.Empty(t, recorder.Body.String())
			if tt.name == "invalid json" {
				assert.NotNil(t, taskData)
			}
		})
	}
}

// 回归测试法：xAI 动态计费只返回 OtherRatios，不应修改 PriceData 本体，避免影响外层通用计费模块。
func TestEstimateBilling_RegressionDoesNotMutatePriceData(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionTextGenerate},
		PriceData: types.PriceData{
			ModelPrice: 1.25,
			Quota:      1250,
		},
	}
	req := relaycommon.TaskSubmitReq{Prompt: "generate", Duration: 4, Metadata: map[string]interface{}{"resolution": "720p"}}

	ratios := adaptor.EstimateBilling(testContextWithTaskRequest(req), info)

	require.NotNil(t, ratios)
	assert.Equal(t, 1.25, info.PriceData.ModelPrice)
	assert.Equal(t, 1250, info.PriceData.Quota)
	assert.Nil(t, info.PriceData.OtherRatios)
}

// 场景法：一次 edit 请求从校验、路由到请求体构建都应保持 xAI 官方 edit 语义。
func TestEditVideoScenario_ValidateRouteAndBuildBody(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt: "make it cinematic",
		Mode:   "edit-video",
		Metadata: map[string]interface{}{
			"video_url":           "https://example.com/input.mp4",
			"input_video_seconds": 20,
		},
	}
	ctx := testContextWithJSONTaskRequest(t, req)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.x.ai", UpstreamModelName: "grok-imagine-video"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(ctx, info)
	require.Nil(t, taskErr)
	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	storedReq, err := relaycommon.GetTaskRequest(ctx)
	require.NoError(t, err)
	body := buildVideoRequest(storedReq, info)

	assert.Equal(t, constant.TaskActionGenerate, info.Action)
	assert.Equal(t, "https://api.x.ai/v1/videos/edits", url)
	assert.Equal(t, "https://example.com/input.mp4", body.VideoURL)
	assert.Nil(t, body.Video)
	assert.Zero(t, body.Duration)
	assert.Empty(t, body.Resolution)
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
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-imagine-video"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"}}

	body := buildVideoRequest(req, info)

	assert.Equal(t, "grok-imagine-video", body.Model)
	assert.Equal(t, "https://example.com/input.mp4", body.VideoURL)
	assert.Nil(t, body.Video)
	assert.Nil(t, body.Image)
	assert.Zero(t, body.Duration)
	assert.Empty(t, body.Resolution)
}

func TestBuildVideoRequestForExtendVideoUsesVideoObject(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Prompt:   "extend it",
		Duration: 6,
		Metadata: map[string]interface{}{
			"mode":      "extend-video",
			"video_url": "https://example.com/input.mp4",
		},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-imagine-video"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "remixGenerate"}}

	body := buildVideoRequest(req, info)

	assert.Equal(t, "grok-imagine-video", body.Model)
	require.NotNil(t, body.Video)
	assert.Equal(t, "https://example.com/input.mp4", body.Video.URL)
	assert.Equal(t, 6, body.Duration)
	assert.Empty(t, body.VideoURL)
	assert.Empty(t, body.Resolution)
	assert.Empty(t, body.AspectRatio)
	assert.Nil(t, body.Image)
	assert.Empty(t, body.ReferenceImages)
}

func TestTopLevelModeRoutesToEditVideo(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt: "make it cinematic",
		Mode:   "edit-video",
		Metadata: map[string]interface{}{
			"video_url": "https://example.com/input.mp4",
		},
	}
	ctx := testContextWithJSONTaskRequest(t, req)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.x.ai"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(ctx, info)

	require.Nil(t, taskErr)
	assert.Equal(t, constant.TaskActionGenerate, info.Action)
	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://api.x.ai/v1/videos/edits", url)
}

func TestValidateRejectsMixedVideoAndImageInputs(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt: "make it cinematic",
		Image:  "https://example.com/image.png",
		Metadata: map[string]interface{}{
			"video_url": "https://example.com/input.mp4",
		},
	}
	ctx := testContextWithJSONTaskRequest(t, req)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	taskErr := adaptor.ValidateRequestAndSetAction(ctx, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}
