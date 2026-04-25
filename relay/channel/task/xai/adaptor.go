package xai

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	defaultDurationSeconds = 8
	defaultResolution      = "480p"
	hdResolution           = "720p"

	baseOutputPricePerSecond = 0.50
	hdOutputPricePerSecond   = 0.70
	inputImagePrice          = 0.02
	inputVideoPricePerSecond = 0.10

	ratioKeySeconds         = "seconds"
	ratioKeyResolution      = "resolution"
	ratioKeyInputAdjustment = "xai_input_cost_adjustment"
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	mode        string
}

type videoRef struct {
	URL string `json:"url,omitempty"`
}

type referenceImage struct {
	URL string `json:"url,omitempty"`
}

type videoRequest struct {
	Model           string           `json:"model"`
	Prompt          string           `json:"prompt,omitempty"`
	Duration        int              `json:"duration,omitempty"`
	AspectRatio     string           `json:"aspect_ratio,omitempty"`
	Resolution      string           `json:"resolution,omitempty"`
	Image           *referenceImage  `json:"image,omitempty"`
	ReferenceImages []referenceImage `json:"reference_images,omitempty"`
	Video           *videoRef        `json:"video,omitempty"`
	VideoURL        string           `json:"video_url,omitempty"`
}

type submitResponse struct {
	RequestID string `json:"request_id"`
	Error     *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

type fetchResponse struct {
	Status string `json:"status"`
	Model  string `json:"model,omitempty"`
	Video  *struct {
		URL               string  `json:"url,omitempty"`
		Duration          float64 `json:"duration,omitempty"`
		RespectModeration *bool   `json:"respect_moderation,omitempty"`
	} `json:"video,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); err != nil {
		return err
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}
	a.mode = resolveMode(req)
	if hasVideoInput(req) && hasImageInput(req) {
		return service.TaskErrorWrapper(fmt.Errorf("xAI video requests cannot include both video and image inputs"), "invalid_request", http.StatusBadRequest)
	}
	info.Action = resolveAction(c, req)
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := strings.TrimRight(a.baseURL, "/")
	switch info.Action {
	case constant.TaskActionRemix:
		return baseURL + "/v1/videos/extensions", nil
	case constant.TaskActionGenerate:
		if a.mode == "edit-video" {
			return baseURL + "/v1/videos/edits", nil
		}
		return baseURL + "/v1/videos/generations", nil
	default:
		return baseURL + "/v1/videos/generations", nil
	}
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, &req.Header)
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	body := buildVideoRequest(req, info)
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(bodyBytes), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	var submit submitResponse
	if err := common.Unmarshal(responseBody, &submit); err != nil {
		return "", responseBody, service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
	}
	if submit.Error != nil && submit.Error.Message != "" {
		return "", responseBody, service.TaskErrorWrapper(fmt.Errorf("%s", submit.Error.Message), "upstream_error", http.StatusBadGateway)
	}
	if strings.TrimSpace(submit.RequestID) == "" {
		return "", responseBody, service.TaskErrorWrapper(fmt.Errorf("missing request_id"), "invalid_response", http.StatusBadGateway)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.CreatedAt = time.Now().Unix()
	video.Model = info.OriginModelName
	c.JSON(http.StatusOK, video)
	return submit.RequestID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	url := fmt.Sprintf("%s/v1/videos/%s", strings.TrimRight(baseURL, "/"), taskID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res fetchResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, fmt.Errorf("unmarshal xai video response failed: %w", err)
	}
	if res.Error != nil && res.Error.Message != "" {
		return &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: res.Error.Message}, nil
	}
	taskInfo := &relaycommon.TaskInfo{}
	switch strings.ToLower(res.Status) {
	case "pending", "queued":
		taskInfo.Status = model.TaskStatusQueued
		taskInfo.Progress = taskcommon.ProgressQueued
	case "processing", "running", "in_progress":
		taskInfo.Status = model.TaskStatusInProgress
		taskInfo.Progress = taskcommon.ProgressInProgress
	case "done", "completed", "succeeded", "success":
		taskInfo.Status = model.TaskStatusSuccess
		taskInfo.Progress = taskcommon.ProgressComplete
		if res.Video != nil {
			taskInfo.Url = res.Video.URL
			if res.Video.Duration > 0 {
				taskInfo.TotalTokens = int(math.Ceil(res.Video.Duration))
			}
		}
	case "expired", "failed", "failure":
		taskInfo.Status = model.TaskStatusFailure
		if res.Error != nil && res.Error.Message != "" {
			taskInfo.Reason = res.Error.Message
		} else {
			taskInfo.Reason = "xAI video task " + res.Status
		}
	default:
		return nil, fmt.Errorf("unknown xai video status: %s", res.Status)
	}
	return taskInfo, nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	seconds := resolveOutputDuration(req)
	resolutionRatio := resolveResolutionRatio(req, info.Action)
	outputPrice := outputPricePerSecond(info)
	inputCost := estimateInputCost(req, info, info.Action)
	outputCost := outputPrice * float64(seconds) * resolutionRatio
	inputAdjustment := 1.0
	if outputCost > 0 && inputCost > 0 {
		inputAdjustment = (outputCost + inputCost) / outputCost
	}
	return map[string]float64{
		ratioKeySeconds:         float64(seconds),
		ratioKeyResolution:      resolutionRatio,
		ratioKeyInputAdjustment: inputAdjustment,
	}
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	if taskResult == nil || taskResult.TotalTokens <= 0 || task.PrivateData.BillingContext == nil {
		return 0
	}
	bc := task.PrivateData.BillingContext
	if bc.ModelPrice <= 0 || bc.GroupRatio <= 0 {
		return 0
	}
	ratios := bc.OtherRatios
	resolutionRatio := ratioOrDefault(ratios, ratioKeyResolution, 1)
	inputAdjustment := ratioOrDefault(ratios, ratioKeyInputAdjustment, 1)
	estimatedSeconds := ratioOrDefault(ratios, ratioKeySeconds, defaultDurationSeconds)
	estimatedOutputCost := bc.ModelPrice * estimatedSeconds * resolutionRatio
	inputCost := estimatedOutputCost * (inputAdjustment - 1)
	if inputCost < 0 {
		inputCost = 0
	}
	actualCost := bc.ModelPrice*float64(taskResult.TotalTokens)*resolutionRatio + inputCost
	return int(actualCost * common.QuotaPerUnit * bc.GroupRatio)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"grok-imagine-video"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "xai-video"
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := dto.NewOpenAIVideo()
	video.ID = task.TaskID
	video.TaskID = task.TaskID
	video.Model = task.Properties.OriginModelName
	video.Status = task.Status.ToVideoStatus()
	video.SetProgressStr(task.Progress)
	video.CreatedAt = task.CreatedAt
	if task.FinishTime > 0 {
		video.CompletedAt = task.FinishTime
	} else if task.UpdatedAt > 0 {
		video.CompletedAt = task.UpdatedAt
	}
	if task.GetResultURL() != "" {
		video.SetMetadata("url", task.GetResultURL())
	}
	var res fetchResponse
	if len(task.Data) > 0 && common.Unmarshal(task.Data, &res) == nil && res.Video != nil && res.Video.Duration > 0 {
		video.Seconds = strconv.FormatFloat(res.Video.Duration, 'f', -1, 64)
	}
	return common.Marshal(video)
}

func resolveAction(c *gin.Context, req relaycommon.TaskSubmitReq) string {
	mode := resolveMode(req)
	if mode == "extend-video" || strings.Contains(c.Request.URL.Path, "/remix") {
		return constant.TaskActionRemix
	}
	if mode == "edit-video" || metadataString(req.Metadata, "video_url") != "" || metadataString(req.Metadata, "videoUrl") != "" {
		return constant.TaskActionGenerate
	}
	if req.HasImage() || metadataString(req.Metadata, "image_url") != "" || len(metadataSlice(req.Metadata, "reference_image_urls")) > 0 || len(metadataSlice(req.Metadata, "referenceImageUrls")) > 0 {
		return constant.TaskActionGenerate
	}
	return constant.TaskActionTextGenerate
}

func buildVideoRequest(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) videoRequest {
	body := videoRequest{
		Model:  info.UpstreamModelName,
		Prompt: req.Prompt,
	}
	if body.Model == "" {
		body.Model = req.Model
	}

	mode := resolveMode(req)
	if info.Action == constant.TaskActionRemix || mode == "extend-video" {
		body.Duration = resolveOutputDuration(req)
		if url := resolveInputVideoURL(req, info); url != "" {
			body.Video = &videoRef{URL: url}
		}
		return body
	}

	if mode == "edit-video" {
		body.VideoURL = resolveInputVideoURL(req, info)
		return body
	}

	body.Duration = resolveOutputDuration(req)
	body.AspectRatio = metadataString(req.Metadata, "aspect_ratio")
	if body.AspectRatio == "" {
		body.AspectRatio = metadataString(req.Metadata, "aspectRatio")
	}
	if body.AspectRatio == "" && req.Size != "" {
		body.AspectRatio = sizeToAspectRatio(req.Size)
	}
	body.Resolution = resolveResolution(req, info.Action)
	if body.Resolution == defaultResolution {
		body.Resolution = ""
	}

	imageURL := firstNonEmpty(req.Image, metadataString(req.Metadata, "image_url"), metadataString(req.Metadata, "imageUrl"))
	if imageURL == "" && len(req.Images) == 1 {
		imageURL = req.Images[0]
	}
	if imageURL != "" {
		body.Image = &referenceImage{URL: imageURL}
	}

	for _, url := range append(metadataSlice(req.Metadata, "reference_image_urls"), metadataSlice(req.Metadata, "referenceImageUrls")...) {
		if url != "" {
			body.ReferenceImages = append(body.ReferenceImages, referenceImage{URL: url})
		}
	}
	if len(body.ReferenceImages) == 0 && len(req.Images) > 1 {
		for _, url := range req.Images {
			if url != "" {
				body.ReferenceImages = append(body.ReferenceImages, referenceImage{URL: url})
			}
		}
	}
	return body
}

func resolveInputVideoURL(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	if url := firstNonEmpty(metadataString(req.Metadata, "video_url"), metadataString(req.Metadata, "videoUrl"), req.InputReference); url != "" {
		return url
	}
	url, _ := originVideoURLAndDuration(info)
	return url
}

func originVideoURLAndDuration(info *relaycommon.RelayInfo) (string, float64) {
	if info == nil || strings.TrimSpace(info.OriginTaskID) == "" || info.UserId <= 0 {
		return "", 0
	}
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil || !exist || originTask == nil {
		return "", 0
	}
	duration := 0.0
	var res fetchResponse
	if len(originTask.Data) > 0 && common.Unmarshal(originTask.Data, &res) == nil && res.Video != nil {
		duration = res.Video.Duration
	}
	return originTask.GetResultURL(), duration
}

func resolveOutputDuration(req relaycommon.TaskSubmitReq) int {
	if req.Duration > 0 {
		return req.Duration
	}
	if seconds, err := strconv.Atoi(req.Seconds); err == nil && seconds > 0 {
		return seconds
	}
	if d := metadataNumber(req.Metadata, "duration"); d > 0 {
		return int(math.Ceil(d))
	}
	return defaultDurationSeconds
}

func resolveResolution(req relaycommon.TaskSubmitReq, action string) string {
	resolution := strings.ToLower(firstNonEmpty(metadataString(req.Metadata, "resolution"), metadataString(req.Metadata, "quality")))
	if resolution == hdResolution {
		return hdResolution
	}
	return defaultResolution
}

func resolveResolutionRatio(req relaycommon.TaskSubmitReq, action string) float64 {
	if resolveResolution(req, action) == hdResolution {
		return hdOutputPricePerSecond / baseOutputPricePerSecond
	}
	return 1
}

func estimateInputCost(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo, action string) float64 {
	cost := 0.0
	if action != constant.TaskActionRemix && resolveMode(req) != "extend-video" && resolveMode(req) != "edit-video" {
		imageCount := countGenerationInputImages(req)
		cost += float64(imageCount) * inputImagePrice
	}
	if action == constant.TaskActionRemix || resolveMode(req) == "extend-video" || resolveMode(req) == "edit-video" {
		seconds := metadataNumber(req.Metadata, "input_video_seconds")
		if seconds <= 0 {
			seconds = metadataNumber(req.Metadata, "inputVideoSeconds")
		}
		if seconds <= 0 {
			_, seconds = originVideoURLAndDuration(info)
		}
		if seconds > 0 {
			cost += seconds * inputVideoPricePerSecond
		}
	}
	return cost
}

func outputPricePerSecond(info *relaycommon.RelayInfo) float64 {
	if info != nil && info.PriceData.ModelPrice > 0 {
		return info.PriceData.ModelPrice
	}
	return baseOutputPricePerSecond
}

func resolveMode(req relaycommon.TaskSubmitReq) string {
	mode := strings.ToLower(strings.TrimSpace(firstNonEmpty(req.Mode, metadataString(req.Metadata, "mode"))))
	if mode == "" && (metadataString(req.Metadata, "video_url") != "" || metadataString(req.Metadata, "videoUrl") != "") {
		return "edit-video"
	}
	return mode
}

func hasVideoInput(req relaycommon.TaskSubmitReq) bool {
	return firstNonEmpty(metadataString(req.Metadata, "video_url"), metadataString(req.Metadata, "videoUrl"), req.InputReference) != ""
}

func hasImageInput(req relaycommon.TaskSubmitReq) bool {
	return req.Image != "" || len(req.Images) > 0 || metadataString(req.Metadata, "image_url") != "" || metadataString(req.Metadata, "imageUrl") != "" || len(metadataSlice(req.Metadata, "reference_image_urls")) > 0 || len(metadataSlice(req.Metadata, "referenceImageUrls")) > 0
}

func countGenerationInputImages(req relaycommon.TaskSubmitReq) int {
	count := 0
	if firstNonEmpty(req.Image, metadataString(req.Metadata, "image_url"), metadataString(req.Metadata, "imageUrl")) != "" {
		count++
	} else if len(req.Images) == 1 {
		count++
	}
	metadataRefs := append(metadataSlice(req.Metadata, "reference_image_urls"), metadataSlice(req.Metadata, "referenceImageUrls")...)
	if len(metadataRefs) > 0 {
		return count + len(metadataRefs)
	}
	if len(req.Images) > 1 {
		count += len(req.Images)
	}
	return count
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func metadataSlice(metadata map[string]interface{}, key string) []string {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil
	}
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

func metadataNumber(metadata map[string]interface{}, key string) float64 {
	if metadata == nil {
		return 0
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
		return f
	}
}

func ratioOrDefault(ratios map[string]float64, key string, fallback float64) float64 {
	if ratios == nil || ratios[key] <= 0 {
		return fallback
	}
	return ratios[key]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sizeToAspectRatio(size string) string {
	parts := strings.SplitN(strings.ToLower(size), "x", 2)
	if len(parts) != 2 {
		return ""
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w <= 0 || h <= 0 {
		return ""
	}
	ratio := float64(w) / float64(h)
	switch {
	case math.Abs(ratio-1) < 0.05:
		return "1:1"
	case ratio > 1.6:
		return "16:9"
	case ratio < 0.65:
		return "9:16"
	case ratio > 1:
		return "4:3"
	default:
		return "3:4"
	}
}
