package htmlparser2

import "strings"

// foreignElementNames and foreignAttributeNames are dom-serializer's foreignNames: the
// mixed-case SVG and MathML names it restores in foreign content.
var foreignElementNames = mixedCase(
	"altGlyph", "altGlyphDef", "altGlyphItem", "animateColor", "animateMotion",
	"animateTransform", "clipPath", "feBlend", "feColorMatrix", "feComponentTransfer",
	"feComposite", "feConvolveMatrix", "feDiffuseLighting", "feDisplacementMap",
	"feDistantLight", "feDropShadow", "feFlood", "feFuncA", "feFuncB", "feFuncG", "feFuncR",
	"feGaussianBlur", "feImage", "feMerge", "feMergeNode", "feMorphology", "feOffset",
	"fePointLight", "feSpecularLighting", "feSpotLight", "feTile", "feTurbulence",
	"foreignObject", "glyphRef", "linearGradient", "radialGradient", "textPath",
)

var foreignAttributeNames = mixedCase(
	"definitionURL", "attributeName", "attributeType", "baseFrequency", "baseProfile",
	"calcMode", "clipPathUnits", "diffuseConstant", "edgeMode", "filterUnits", "glyphRef",
	"gradientTransform", "gradientUnits", "kernelMatrix", "kernelUnitLength", "keyPoints",
	"keySplines", "keyTimes", "lengthAdjust", "limitingConeAngle", "markerHeight",
	"markerUnits", "markerWidth", "maskContentUnits", "maskUnits", "numOctaves",
	"pathLength", "patternContentUnits", "patternTransform", "patternUnits", "pointsAtX",
	"pointsAtY", "pointsAtZ", "preserveAlpha", "preserveAspectRatio", "primitiveUnits",
	"refX", "refY", "repeatCount", "repeatDur", "requiredExtensions", "requiredFeatures",
	"specularConstant", "specularExponent", "spreadMethod", "startOffset", "stdDeviation",
	"stitchTiles", "surfaceScale", "systemLanguage", "tableValues", "targetX", "targetY",
	"textLength", "viewBox", "viewTarget", "xChannelSelector", "yChannelSelector",
	"zoomAndPan",
)

// foreignIntegrationPoints are the foreign elements whose children dom-serializer
// writes as HTML again.
var foreignIntegrationPoints = map[string]bool{
	"mi": true, "mo": true, "mn": true, "ms": true, "mtext": true, "annotation-xml": true,
	"foreignObject": true, "desc": true, "title": true,
}

func mixedCase(names ...string) map[string]string {
	m := make(map[string]string, len(names))
	for _, n := range names {
		m[strings.ToLower(n)] = n
	}
	return m
}
