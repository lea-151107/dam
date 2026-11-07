package mermaid

// Version is the library version. It changes whenever rendering output may
// change, so callers can use it as part of a cache key. The key must also
// cover the Options that affect the output (MaxWidth, Scale, colors, the
// font) and, if the caller's build may resolve a different
// golang.org/x/image or golang.org/x/text, those versions too:
// rasterization and downscaling use x/image, and composing combining marks
// in the input uses the Unicode tables of x/text.
const Version = "0.6.34"
