// Package styles provides centralized styling for Steep UI.
package styles

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

func init() {
	// Register the Steep syntax theme optimized for SQL
	styles.Register(SteepTheme)
	styles.Register(SteepLightTheme)
}

// SteepTheme is a dark theme optimized for SQL readability
var SteepTheme = chroma.MustNewStyle("steep", chroma.StyleEntries{
	// Background and defaults
	chroma.Background:       "bg:#1a1a2e",
	chroma.Text:             "#eaeaea",
	chroma.Error:            "#ff5555 bold",

	// Keywords: SELECT, FROM, WHERE, JOIN, etc. - Cyan, bold
	chroma.Keyword:          "bold #50fa7b",
	chroma.KeywordConstant:  "#bd93f9",
	chroma.KeywordNamespace: "bold #50fa7b", // SCHEMA, DATABASE
	chroma.KeywordType:      "#8be9fd",      // INT, VARCHAR, etc.

	// Strings: 'hello world' - Yellow/Gold
	chroma.String:        "#f1fa8c",
	chroma.StringEscape:  "#ffb86c",
	chroma.StringSymbol:  "#f1fa8c",

	// Numbers: 42, 3.14 - Purple
	chroma.Number:      "#bd93f9",
	chroma.NumberFloat: "#bd93f9",

	// Functions: COUNT, MAX, SUM, COALESCE - Pink
	chroma.NameFunction: "#ff79c6",
	chroma.NameBuiltin:  "#ff79c6",

	// Operators: =, <>, +, -, AND, OR - Cyan
	chroma.Operator:     "#8be9fd",
	chroma.OperatorWord: "bold #ff79c6", // AND, OR, NOT

	// Comments: -- this is a comment - Gray, italic
	chroma.Comment:          "italic #6272a4",
	chroma.CommentSingle:    "italic #6272a4",
	chroma.CommentMultiline: "italic #6272a4",

	// Punctuation: (), ;, , - Subtle
	chroma.Punctuation: "#f8f8f2",

	// Names/Identifiers: table names, column names
	chroma.Name:          "#f8f8f2",
	chroma.NameVariable:  "#f8f8f2",
	chroma.NameAttribute: "#50fa7b",
	chroma.NameClass:     "#8be9fd", // Table names in some contexts
	chroma.NameConstant:  "#bd93f9", // TRUE, FALSE, NULL

	// Generic tokens
	chroma.GenericHeading:    "bold #f8f8f2",
	chroma.GenericSubheading: "bold #6272a4",
	chroma.GenericDeleted:    "#ff5555",
	chroma.GenericInserted:   "#50fa7b",
	chroma.GenericEmph:       "italic",
	chroma.GenericStrong:     "bold",
})

// SteepLightTheme is a light theme variant
var SteepLightTheme = chroma.MustNewStyle("steep-light", chroma.StyleEntries{
	chroma.Background: "bg:#fafafa",
	chroma.Text:       "#383a42",

	chroma.Keyword:          "bold #a626a4",
	chroma.KeywordConstant:  "#986801",
	chroma.KeywordNamespace: "bold #a626a4",
	chroma.KeywordType:      "#0184bc",

	chroma.String:       "#50a14f",
	chroma.StringEscape: "#986801",

	chroma.Number:      "#986801",
	chroma.NumberFloat: "#986801",

	chroma.NameFunction: "#4078f2",
	chroma.NameBuiltin:  "#4078f2",

	chroma.Operator:     "#383a42",
	chroma.OperatorWord: "bold #a626a4",

	chroma.Comment:       "italic #a0a1a7",
	chroma.CommentSingle: "italic #a0a1a7",

	chroma.Punctuation:   "#383a42",
	chroma.Name:          "#383a42",
	chroma.NameVariable:  "#e45649",
	chroma.NameAttribute: "#986801",
	chroma.NameConstant:  "#986801",
})
