package sparkplug

import (
	"fmt"
	"strings"
)

// BuildTopic constructs a Sparkplug B topic from components
// Format: spBv1.0/{group_id}/{message_type}/{edge_node_id}/{device_id}
func BuildTopic(groupID, messageType, edgeNodeID, deviceID string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s",
		Namespace,
		slugify(groupID),
		messageType,
		slugify(edgeNodeID),
		slugify(deviceID),
	)
}

// BuildDDATATopic constructs a DDATA (data) topic for publishing tag values
func BuildDDATATopic(groupID, edgeNodeID, deviceID string) string {
	return BuildTopic(groupID, MessageTypeDDATA, edgeNodeID, deviceID)
}

// BuildDBIRTHTopic constructs a DBIRTH (device birth) topic
func BuildDBIRTHTopic(groupID, edgeNodeID, deviceID string) string {
	return BuildTopic(groupID, MessageTypeDBIRTH, edgeNodeID, deviceID)
}

// BuildDDEATHTopic constructs a DDEATH (device death) topic
func BuildDDEATHTopic(groupID, edgeNodeID, deviceID string) string {
	return BuildTopic(groupID, MessageTypeDDEATH, edgeNodeID, deviceID)
}

// BuildNBIRTHTopic constructs a NBIRTH (node birth) topic
func BuildNBIRTHTopic(groupID, edgeNodeID string) string {
	return fmt.Sprintf("%s/%s/%s/%s",
		Namespace,
		slugify(groupID),
		MessageTypeNBIRTH,
		slugify(edgeNodeID),
	)
}

// BuildNDEATHTopic constructs a NDEATH (node death) topic
func BuildNDEATHTopic(groupID, edgeNodeID string) string {
	return fmt.Sprintf("%s/%s/%s/%s",
		Namespace,
		slugify(groupID),
		MessageTypeNDEATH,
		slugify(edgeNodeID),
	)
}

// ParseTopic parses a Sparkplug B topic and returns its components
func ParseTopic(topic string) (*TopicInfo, error) {
	parts := strings.Split(topic, "/")
	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid Sparkplug B topic format: %s", topic)
	}

	if parts[0] != Namespace {
		return nil, fmt.Errorf("not a Sparkplug B topic (expected namespace %s): %s", Namespace, topic)
	}

	info := &TopicInfo{
		Namespace:   parts[0],
		GroupID:     parts[1],
		MessageType: parts[2],
		EdgeNodeID:  parts[3],
	}

	if len(parts) >= 6 {
		info.DeviceID = parts[4]
	}

	return info, nil
}

// IsSparkplugTopic checks if a topic is a Sparkplug B topic
func IsSparkplugTopic(topic string) bool {
	return strings.HasPrefix(topic, Namespace+"/")
}

// BuildGroupID creates a group ID from organization and site names
// Format: {org}-{site} (e.g., "acme-corp-factory1")
func BuildGroupID(orgName, siteName string) string {
	return fmt.Sprintf("%s-%s", slugify(orgName), slugify(siteName))
}

// BuildEdgeNodeID creates an edge node ID from area and gateway names
// Format: {area}-{gateway} (e.g., "production-plc01")
func BuildEdgeNodeID(areaName, gatewayName string) string {
	return fmt.Sprintf("%s-%s", slugify(areaName), slugify(gatewayName))
}

// BuildLegacyTopic constructs a legacy topic for backward compatibility
// Format: data/{org}/{site}/{area}/{gateway}/{alias}
func BuildLegacyTopic(org, site, area, gateway, alias string) string {
	return fmt.Sprintf("data/%s/%s/%s/%s/%s",
		slugify(org),
		slugify(site),
		slugify(area),
		slugify(gateway),
		slugify(alias),
	)
}

// ExtractLegacyTopicInfo parses a legacy topic and returns its components
func ExtractLegacyTopicInfo(topic string) (org, site, area, gateway, alias string, err error) {
	parts := strings.Split(topic, "/")
	if len(parts) < 6 || parts[0] != "data" {
		err = fmt.Errorf("invalid legacy topic format: %s", topic)
		return
	}

	org = parts[1]
	site = parts[2]
	area = parts[3]
	gateway = parts[4]
	alias = parts[5]
	return
}

// slugify converts a string to URL-friendly format
func slugify(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
