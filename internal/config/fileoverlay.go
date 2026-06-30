package config

import "gopkg.in/yaml.v3"

// FileOverlayFromNode builds an Overlay that marks only the fields
// actually present in the YAML document node as set. cfg supplies the
// decoded values; root is the yaml.Node tree from the same document.
//
// This is the precise form of FileOverlay: it can distinguish "user
// wrote track.lts: false" from "user omitted track.lts entirely",
// which yaml.v3 cannot do by reflection alone.
//
// Unknown top-level keys (those not in the schema) are ignored; the
// YAML decoder itself does not error on them. Callers that want to
// warn on typos can inspect root.Content themselves.
func FileOverlayFromNode(cfg *Config, root *yaml.Node) *Overlay {
	if cfg == nil {
		return NewOverlay()
	}
	o := &Overlay{C: cfg}
	if root == nil {
		return o // all unset
	}
	// root may be a Document node wrapping a Mapping — descend.
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return o
	}
	for i := 0; i < len(doc.Content)-1; i += 2 {
		keyNode := doc.Content[i]
		valNode := doc.Content[i+1]
		switch keyNode.Value {
		case "schema_version":
			o.SchemaVersionSet = true
			o.C.SchemaVersion = decodeInt(valNode)
		case "manager":
			o.ManagerSet = true
			o.C.Manager = decodeString(valNode)
		case "track":
			applyTrackOverlay(o.C, valNode, &o.TrackLTSSet, &o.TrackCurrentSet)
		case "packages":
			applyPackagesOverlay(o.C, valNode,
				&o.PackagesMigrateSet, &o.PackagesStrategySet, &o.PackagesSkipSet)
		case "cleanup":
			applyCleanupOverlay(o.C, valNode, &o.CleanupAutoSet, &o.CleanupPromptSet)
		case "cache":
			applyCacheOverlay(o.C, valNode, &o.CacheTTLSet)
		}
	}
	return o
}

// applyTrackOverlay scans a track-mapping node and sets the
// corresponding overlay flags + values on cfg.
func applyTrackOverlay(cfg *Config, node *yaml.Node, ltsSet, currentSet *bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		switch node.Content[i].Value {
		case "lts":
			*ltsSet = true
			cfg.Track.LTS = decodeBool(node.Content[i+1])
		case "current":
			*currentSet = true
			cfg.Track.Current = decodeBool(node.Content[i+1])
		}
	}
}

// applyPackagesOverlay scans a packages-mapping node.
func applyPackagesOverlay(cfg *Config, node *yaml.Node, migrateSet, strategySet, skipSet *bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		switch node.Content[i].Value {
		case "migrate":
			*migrateSet = true
			cfg.Packages.Migrate = decodeBool(node.Content[i+1])
		case "strategy":
			*strategySet = true
			cfg.Packages.Strategy = Strategy(decodeString(node.Content[i+1]))
		case "skip":
			*skipSet = true
			cfg.Packages.Skip = decodeStringList(node.Content[i+1])
		}
	}
}

// applyCleanupOverlay scans a cleanup-mapping node.
func applyCleanupOverlay(cfg *Config, node *yaml.Node, autoSet, promptSet *bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		switch node.Content[i].Value {
		case "auto":
			*autoSet = true
			cfg.Cleanup.Auto = decodeBool(node.Content[i+1])
		case "prompt":
			*promptSet = true
			cfg.Cleanup.Prompt = decodeBool(node.Content[i+1])
		}
	}
}

// applyCacheOverlay scans a cache-mapping node.
func applyCacheOverlay(cfg *Config, node *yaml.Node, ttlSet *bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == "ttl" {
			*ttlSet = true
			cfg.Cache.TTL = decodeInt(node.Content[i+1])
		}
	}
}

// decodeBool returns the bool value of a scalar node, defaulting to
// false on unexpected types. This matches the behavior of the
// subsequent yaml.Unmarshal call for the same value.
func decodeBool(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	var b bool
	_ = n.Decode(&b)
	return b
}

// decodeString returns the string value of a scalar node.
func decodeString(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	var s string
	_ = n.Decode(&s)
	return s
}

// decodeInt returns the int value of a scalar node.
func decodeInt(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	var i int
	_ = n.Decode(&i)
	return i
}

// decodeStringList returns a []string from a sequence node.
func decodeStringList(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for _, item := range n.Content {
		var s string
		_ = item.Decode(&s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
