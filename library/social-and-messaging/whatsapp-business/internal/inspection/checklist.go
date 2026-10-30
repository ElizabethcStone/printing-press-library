package inspection

import (
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/captures"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/metamcp"
	"regexp"
	"strings"
)

var placeholders = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)
var position = regexp.MustCompile(`^[1-9][0-9]*$`)

// TemplateChecklist extracts required values, not review examples, from one
// template and checks only the parameter channels documented by the send tool.
func TemplateChecklist(snapshot captures.Snapshot, sendTool metamcp.Tool) (map[string]any, error) {
	if sendTool.Name != "whatsapp_biz_send_message" {
		return nil, fmt.Errorf("template checklist requires the WhatsApp send_message tool contract")
	}
	t, err := template(snapshot)
	if err != nil {
		return nil, err
	}
	props := object(sendTool.InputSchema["properties"])
	requirements, unsupported, components := []map[string]any{}, []map[string]any{}, []map[string]any{}
	flag := func(path, reason, classification string) {
		unsupported = append(unsupported, map[string]any{"path": path, "reason": reason, "classification": classification})
	}
	channelSupport := func(channel string) string {
		if _, ok := props[channel]; !ok {
			return "unsupported"
		}
		return "supported"
	}
	add := func(path, key, channel, support string) {
		requirements = append(requirements, map[string]any{"path": path, "placeholder": key, "channel": channel, "support": support})
		if support != "supported" {
			flag(path, "the captured MCP send contract does not establish this value mapping", support)
		}
	}
	named := strings.EqualFold(text(t["parameter_format"]), "NAMED")
	extract := func(value, path, channel, base string, one bool) {
		seen := map[string]bool{}
		for _, match := range placeholders.FindAllStringSubmatch(value, -1) {
			key := strings.TrimSpace(match[1])
			if seen[key] {
				continue
			}
			seen[key] = true
			support := base
			if named || !position.MatchString(key) || (one && key != "1") {
				support = "unsupported"
			}
			add(path, key, channel, support)
		}
	}
	rows, ok := t["components"].([]any)
	if !ok {
		flag("/components", "template components are missing or have an unknown shape", "unknown")
	}
	for i, value := range rows {
		c := object(value)
		path := fmt.Sprintf("/components/%d", i)
		kind := strings.ToUpper(text(c["type"]))
		components = append(components, map[string]any{"path": path, "type": kind, "format": c["format"]})
		switch kind {
		case "BODY":
			body, exists := c["text"].(string)
			if !exists {
				flag(path, "body text is not available for placeholder extraction", "unknown")
				continue
			}
			extract(body, path+"/text", "body_parameters", channelSupport("body_parameters"), false)
		case "HEADER":
			format := strings.ToUpper(text(c["format"]))
			support := channelSupport("header_parameter")
			switch format {
			case "TEXT":
				header, exists := c["text"].(string)
				if !exists {
					flag(path, "text header content is missing", "unknown")
					continue
				}
				extract(header, path+"/text", "header_parameter", support, true)
			case "IMAGE", "VIDEO", "DOCUMENT", "LOCATION":
				add(path+"/value", strings.ToLower(format), "header_parameter", support)
			default:
				flag(path, "header format is unknown or unsupported by the captured send contract", "unknown")
				extract(text(c["text"]), path+"/text", "header_parameter", "unknown", true)
			}
		case "FOOTER":
			extract(text(c["text"]), path+"/text", "", "unsupported", false)
		case "BUTTONS":
			buttons, exists := c["buttons"].([]any)
			if !exists {
				flag(path, "button components have an unknown shape", "unknown")
				continue
			}
			for j, value := range buttons {
				button := object(value)
				bp := fmt.Sprintf("%s/buttons/%d", path, j)
				bt := strings.ToUpper(text(button["type"]))
				support := channelSupport("button_parameters")
				switch bt {
				case "URL":
					url, exists := button["url"].(string)
					if !exists {
						flag(bp, "URL button is missing its URL", "unknown")
						continue
					}
					matches := placeholders.FindAllStringIndex(url, -1)
					if len(matches) > 1 || (len(matches) == 1 && matches[0][1] != len(url)) {
						support = "unsupported"
					}
					extract(url, bp+"/url", "button_parameters", support, true)
				case "QUICK_REPLY":
					add(bp+"/payload", "payload", "button_parameters", support)
				case "COPY_CODE":
					add(bp+"/coupon_code", "coupon_code", "button_parameters", support)
				case "PHONE_NUMBER": // Static template value: no runtime value channel.
				case "FLOW":
					flag(bp, "flow-token button values require the business Cloud API integration", "unsupported")
				default:
					flag(bp, "button type is unknown or not expressible by the captured send contract", "unknown")
				}
			}
		case "CAROUSEL", "LIMITED_TIME_OFFER":
			flag(path, "this component requires full components outside the MCP send channels", "unsupported")
		default:
			flag(path, "component type is unknown or not supported by the captured send contract", "unknown")
		}
	}
	return map[string]any{"capture": evidence(snapshot), "template_id": t["id"], "template_name": t["name"], "parameter_format": t["parameter_format"], "components": components, "requirements": requirements, "unsupported": unsupported, "note": "Values must come from the user or actual business data. Review examples are excluded. This checklist does not establish send eligibility, approval freshness, pricing or delivery."}, nil
}
