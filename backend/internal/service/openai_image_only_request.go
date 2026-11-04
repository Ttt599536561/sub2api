package service

import (
	"slices"

	"github.com/tidwall/gjson"
)

// OpenAIResponsesImageOnlyEstimate identifies requests eligible for the HTTP
// image-only bridge. Account routing and flat pricing still need confirmation.
func OpenAIResponsesImageOnlyEstimate(model string, body []byte) (imageOnly bool, toolModel string) {
	if !isOpenAIImageGenerationModel(model) || !gjson.ValidBytes(body) {
		return false, ""
	}
	fields := uniqueImageRequestFields(parseRawJSONView(body), "model", "input", "prompt", "tool_choice", "tools")
	if fields == nil || !isOpenAIImageGenerationModel(openAIJSONString(fields["model"])) {
		return false, ""
	}
	input := fields["input"]
	if input.Exists() {
		if !input.IsArray() && openAIJSONString(input) == "" {
			return false, ""
		}
	} else if openAIJSONString(fields["prompt"]) == "" {
		return false, ""
	}
	// The bridge inserts the forced native choice only when choice is absent.
	if choice := fields["tool_choice"]; choice.Exists() {
		choiceFields := uniqueImageRequestFields(choice, "type")
		if choiceFields["type"].Str != "image_generation" {
			return false, ""
		}
	}
	tools := fields["tools"]
	if !tools.Exists() {
		return true, ""
	}
	if !tools.IsArray() {
		return false, ""
	}
	valid, found := true, false
	tools.ForEach(func(_, tool gjson.Result) bool {
		fields := uniqueImageRequestFields(tool, "type", "model")
		kind := openAIJSONString(fields["type"])
		if kind == "" {
			valid = false
			return false
		}
		if kind == "image_generation" {
			model := fields["model"]
			// Match the native tool type and trimmed model read by
			// resolveOpenAIResponsesImageBillingConfigDetailedFromBody.
			if found || (model.Exists() && openAIJSONString(model) == "") {
				valid = false
				return false
			}
			found, toolModel = true, openAIJSONString(model)
		}
		return true
	})
	if !valid {
		return false, ""
	}
	return true, toolModel
}

// JSON views use the first duplicate key, while the bridge's decoded map
// uses the last. Reject duplicates in fields used to classify or price images.
func uniqueImageRequestFields(object gjson.Result, keys ...string) map[string]gjson.Result {
	if !object.IsObject() {
		return nil
	}
	fields := make(map[string]gjson.Result, len(keys))
	object.ForEach(func(key, value gjson.Result) bool {
		if slices.Contains(keys, key.Str) {
			if _, seen := fields[key.Str]; seen {
				fields = nil
				return false
			}
			fields[key.Str] = value
		}
		return true
	})
	return fields
}
