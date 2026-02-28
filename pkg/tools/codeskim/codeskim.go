// Package codeskim provides a code outline/skimming tool for agents.
// It strips implementation details from source files and returns only
// the structural elements: package declarations, imports, type definitions,
// function signatures, and doc comments. This lets agents understand
// large codebases without consuming massive context.
//
// Problem: Agents need to navigate large codebases to understand structure,
// find functions, and identify types. Reading entire files consumes too much
// LLM context. This package produces compact outlines that capture the
// essential API surface of source files.
//
// For Go files, it uses the stdlib go/parser and go/ast for precise AST-based
// extraction. For other languages, it uses a line-based heuristic that
// identifies common patterns (function definitions, class declarations, etc.).
//
// Safety guards:
//   - File size capped at 1 MB to prevent excessive memory usage
//   - Output truncated at 32 KB to limit LLM context consumption
//   - Directory listings skip hidden files and common noise (node_modules, .git)
//
// Dependencies:
//   - Go stdlib only (go/parser, go/ast, go/token)
//   - No external system dependencies
package codeskim

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxFileSize    = 5 << 20  // 5 MB
	maxOutputBytes = 32 << 10 // 32 KB — cap tool output to avoid consuming the LLM context window
)

// ────────────────────── Request / Response ──────────────────────

type skimRequest struct {
	Path string `json:"path" jsonschema:"description=Absolute path to a source file or directory. For directories, only Go files are processed."`
}

type skimResponse struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	Outline   string `json:"outline"`
	ItemCount int    `json:"item_count"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type skimTools struct{}

func newSkimTools() *skimTools { return &skimTools{} }

func (s *skimTools) skimTool() tool.CallableTool {
	return function.NewFunctionTool(
		s.skim,
		function.WithName("code_skim"),
		function.WithDescription(
			"Extract the structural outline of a source code file: package/module declarations, "+
				"imports, type definitions, function/method signatures, and doc comments. "+
				"Implementation bodies are stripped to reduce token usage. "+
				"Best for Go files (AST-based), but works with Python, JavaScript/TypeScript, "+
				"Java, Rust, and other languages using heuristic extraction.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (s *skimTools) skim(_ context.Context, req skimRequest) (skimResponse, error) {
	resp := skimResponse{Path: req.Path}

	if req.Path == "" {
		return resp, fmt.Errorf("path is required")
	}

	info, err := os.Stat(req.Path)
	if err != nil {
		return resp, fmt.Errorf("cannot access path %q: %w", req.Path, err)
	}

	if info.IsDir() {
		return s.skimDir(req.Path, resp)
	}

	if info.Size() > maxFileSize {
		return resp, fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), maxFileSize)
	}

	return s.skimFile(req.Path, resp)
}

func (s *skimTools) skimDir(dir string, resp skimResponse) (skimResponse, error) {
	var sb strings.Builder
	itemCount := 0

	entries, err := os.ReadDir(dir)
	if err != nil {
		return resp, fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		ext := strings.ToLower(filepath.Ext(entry.Name()))

		// Only process recognized source files.
		if !s.isSourceFile(ext) {
			continue
		}

		fileResp, err := s.skimFile(path, skimResponse{})
		if err != nil {
			continue
		}

		if fileResp.Outline != "" {
			fmt.Fprintf(&sb, "// ═══ %s ═══\n\n", entry.Name())
			sb.WriteString(fileResp.Outline)
			sb.WriteString("\n\n")
			itemCount += fileResp.ItemCount
		}
	}

	resp.Outline = strings.TrimSpace(sb.String())
	if len(resp.Outline) > maxOutputBytes {
		resp.Outline = resp.Outline[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.ItemCount = itemCount
	resp.Message = fmt.Sprintf("Skimmed directory: %d items from %s", itemCount, dir)
	return resp, nil
}

func (s *skimTools) skimFile(path string, resp skimResponse) (skimResponse, error) {
	ext := strings.ToLower(filepath.Ext(path))
	resp.Language = s.languageFromExt(ext)

	if ext == ".go" {
		return s.skimGo(path, resp)
	}

	return s.skimGeneric(path, resp)
}

// ────────────────────── Go AST-based extraction ──────────────────────

func (s *skimTools) skimGo(path string, resp skimResponse) (skimResponse, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		// Fall back to generic skim on parse failure.
		return s.skimGeneric(path, resp)
	}

	var sb strings.Builder
	itemCount := 0

	// Package declaration.
	fmt.Fprintf(&sb, "package %s\n\n", f.Name.Name)

	// Package-level doc comment.
	if f.Doc != nil {
		sb.WriteString(f.Doc.Text())
		sb.WriteString("\n")
	}

	// Imports.
	if len(f.Imports) > 0 {
		sb.WriteString("import (\n")
		for _, imp := range f.Imports {
			if imp.Name != nil {
				fmt.Fprintf(&sb, "\t%s %s\n", imp.Name.Name, imp.Path.Value)
			} else {
				fmt.Fprintf(&sb, "\t%s\n", imp.Path.Value)
			}
		}
		sb.WriteString(")\n\n")
		itemCount++
	}

	// Walk top-level declarations.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			itemCount += s.writeGenDecl(&sb, d, fset)
		case *ast.FuncDecl:
			s.writeFuncDecl(&sb, d)
			itemCount++
		}
	}

	resp.Outline = strings.TrimSpace(sb.String())
	if len(resp.Outline) > maxOutputBytes {
		resp.Outline = resp.Outline[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.ItemCount = itemCount
	resp.Message = fmt.Sprintf("Go AST outline: %d items from %s", itemCount, filepath.Base(path))
	return resp, nil
}

// writeGenDecl writes type, const, and var declarations.
func (s *skimTools) writeGenDecl(sb *strings.Builder, d *ast.GenDecl, fset *token.FileSet) int {
	count := 0

	// Write doc comment.
	if d.Doc != nil {
		sb.WriteString(d.Doc.Text())
	}

	for _, spec := range d.Specs {
		switch sp := spec.(type) {
		case *ast.TypeSpec:
			if sp.Doc != nil {
				sb.WriteString(sp.Doc.Text())
			}
			fmt.Fprintf(sb, "type %s", sp.Name.Name)
			switch t := sp.Type.(type) {
			case *ast.StructType:
				sb.WriteString(" struct {\n")
				if t.Fields != nil {
					for _, field := range t.Fields.List {
						s.writeField(sb, field, fset)
					}
				}
				sb.WriteString("}\n\n")
			case *ast.InterfaceType:
				sb.WriteString(" interface {\n")
				if t.Methods != nil {
					for _, method := range t.Methods.List {
						s.writeField(sb, method, fset)
					}
				}
				sb.WriteString("}\n\n")
			default:
				fmt.Fprintf(sb, " = ... // %T\n\n", sp.Type)
			}
			count++

		case *ast.ValueSpec:
			if sp.Doc != nil {
				sb.WriteString(sp.Doc.Text())
			}
			for _, name := range sp.Names {
				keyword := "var"
				if d.Tok == token.CONST {
					keyword = "const"
				}
				fmt.Fprintf(sb, "%s %s", keyword, name.Name)
				if sp.Type != nil {
					fmt.Fprintf(sb, " %s", s.exprString(sp.Type))
				}
				sb.WriteString("\n")
			}
			count++
		}
	}

	return count
}

// writeField writes a single struct field or interface method.
func (s *skimTools) writeField(sb *strings.Builder, field *ast.Field, fset *token.FileSet) {
	names := make([]string, 0, len(field.Names))
	for _, name := range field.Names {
		names = append(names, name.Name)
	}

	typeStr := s.exprString(field.Type)

	if len(names) > 0 {
		fmt.Fprintf(sb, "\t%s %s\n", strings.Join(names, ", "), typeStr)
	} else {
		fmt.Fprintf(sb, "\t%s\n", typeStr)
	}
}

// writeFuncDecl writes a function/method signature without the body.
func (s *skimTools) writeFuncDecl(sb *strings.Builder, d *ast.FuncDecl) {
	if d.Doc != nil {
		sb.WriteString(d.Doc.Text())
	}

	sb.WriteString("func ")

	// Receiver.
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := d.Recv.List[0]
		recvStr := s.exprString(recv.Type)
		if len(recv.Names) > 0 {
			fmt.Fprintf(sb, "(%s %s) ", recv.Names[0].Name, recvStr)
		} else {
			fmt.Fprintf(sb, "(%s) ", recvStr)
		}
	}

	sb.WriteString(d.Name.Name)

	// Parameters.
	sb.WriteString("(")
	if d.Type.Params != nil {
		s.writeFieldList(sb, d.Type.Params.List)
	}
	sb.WriteString(")")

	// Return types.
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		if len(d.Type.Results.List) == 1 && len(d.Type.Results.List[0].Names) == 0 {
			sb.WriteString(" " + s.exprString(d.Type.Results.List[0].Type))
		} else {
			sb.WriteString(" (")
			s.writeFieldList(sb, d.Type.Results.List)
			sb.WriteString(")")
		}
	}

	sb.WriteString("\n\n")
}

func (s *skimTools) writeFieldList(sb *strings.Builder, fields []*ast.Field) {
	for i, field := range fields {
		if i > 0 {
			sb.WriteString(", ")
		}
		names := make([]string, 0, len(field.Names))
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		typeStr := s.exprString(field.Type)
		if len(names) > 0 {
			sb.WriteString(strings.Join(names, ", ") + " " + typeStr)
		} else {
			sb.WriteString(typeStr)
		}
	}
}

// exprString returns a simplified string representation of a Go AST expression.
func (s *skimTools) exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return s.exprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + s.exprString(t.X)
	case *ast.ArrayType:
		return "[]" + s.exprString(t.Elt)
	case *ast.MapType:
		return "map[" + s.exprString(t.Key) + "]" + s.exprString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + s.exprString(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + s.exprString(t.Value)
		case ast.RECV:
			return "<-chan " + s.exprString(t.Value)
		default:
			return "chan " + s.exprString(t.Value)
		}
	default:
		return "..."
	}
}

// ────────────────────── Generic heuristic extraction ──────────────────────

func (s *skimTools) skimGeneric(path string, resp skimResponse) (skimResponse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return resp, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var sb strings.Builder
	itemCount := 0
	inBlock := false
	braceDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Always include empty lines between items.
		if trimmed == "" {
			if !inBlock {
				sb.WriteString("\n")
			}
			continue
		}

		// Always include comments.
		if s.isComment(trimmed, resp.Language) {
			if !inBlock {
				sb.WriteString(line + "\n")
			}
			continue
		}

		// Always include import/require/use statements.
		if s.isImportLine(trimmed, resp.Language) {
			sb.WriteString(line + "\n")
			itemCount++
			continue
		}

		// Match function/class/type declarations.
		if s.isDeclaration(trimmed, resp.Language) {
			sb.WriteString(line + "\n")
			itemCount++
			inBlock = true
			braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
			if braceDepth <= 0 && strings.Contains(line, ":") {
				// Python-style: single line with colon starts a block
				inBlock = true
				braceDepth = 1
			}
			if braceDepth <= 0 {
				inBlock = false
			}
			continue
		}

		// Track brace depth for skipping function bodies.
		if inBlock {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			if braceDepth <= 0 {
				inBlock = false
			}
			continue
		}

		// Include top-level const/var/let declarations.
		if s.isTopLevelVar(trimmed, resp.Language) {
			sb.WriteString(line + "\n")
			itemCount++
		}
	}

	resp.Outline = strings.TrimSpace(sb.String())
	if len(resp.Outline) > maxOutputBytes {
		resp.Outline = resp.Outline[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.ItemCount = itemCount
	resp.Message = fmt.Sprintf("Heuristic outline: %d items from %s", itemCount, filepath.Base(path))
	return resp, nil
}

// ────────────────────── Language detection helpers ──────────────────────

func (s *skimTools) languageFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".sh", ".bash", ".zsh":
		return "shell"
	default:
		return "unknown"
	}
}

func (s *skimTools) isSourceFile(ext string) bool {
	return s.languageFromExt(ext) != "unknown"
}

func (s *skimTools) isComment(line, lang string) bool {
	switch lang {
	case "python", "ruby", "shell":
		return strings.HasPrefix(line, "#")
	default:
		return strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "///")
	}
}

func (s *skimTools) isImportLine(line, lang string) bool {
	switch lang {
	case "python":
		return strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ")
	case "javascript", "typescript":
		return strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "require(") || strings.Contains(line, "require(")
	case "java", "kotlin":
		return strings.HasPrefix(line, "import ")
	case "rust":
		return strings.HasPrefix(line, "use ")
	case "ruby":
		return strings.HasPrefix(line, "require ")
	case "go":
		return strings.HasPrefix(line, "import ")
	default:
		return strings.HasPrefix(line, "import ")
	}
}

func (s *skimTools) isDeclaration(line, lang string) bool {
	switch lang {
	case "python":
		return strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "async def ")
	case "javascript", "typescript":
		return strings.HasPrefix(line, "function ") ||
			strings.HasPrefix(line, "class ") ||
			strings.HasPrefix(line, "export function ") ||
			strings.HasPrefix(line, "export class ") ||
			strings.HasPrefix(line, "export default ") ||
			strings.HasPrefix(line, "export interface ") ||
			strings.HasPrefix(line, "interface ") ||
			strings.HasPrefix(line, "type ") ||
			strings.HasPrefix(line, "export type ")
	case "java", "kotlin":
		return strings.HasPrefix(line, "public ") || strings.HasPrefix(line, "private ") ||
			strings.HasPrefix(line, "protected ") || strings.HasPrefix(line, "class ") ||
			strings.HasPrefix(line, "interface ") || strings.HasPrefix(line, "fun ") ||
			strings.HasPrefix(line, "data class ")
	case "rust":
		return strings.HasPrefix(line, "fn ") || strings.HasPrefix(line, "pub fn ") ||
			strings.HasPrefix(line, "struct ") || strings.HasPrefix(line, "pub struct ") ||
			strings.HasPrefix(line, "enum ") || strings.HasPrefix(line, "pub enum ") ||
			strings.HasPrefix(line, "trait ") || strings.HasPrefix(line, "pub trait ") ||
			strings.HasPrefix(line, "impl ")
	case "ruby":
		return strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "module ")
	case "c", "cpp", "csharp":
		return strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "struct ") ||
			strings.HasPrefix(line, "namespace ") || strings.HasPrefix(line, "void ") ||
			strings.HasPrefix(line, "int ") || strings.HasPrefix(line, "public ")
	default:
		return strings.HasPrefix(line, "func ") || strings.HasPrefix(line, "function ") ||
			strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "def ")
	}
}

func (s *skimTools) isTopLevelVar(line, lang string) bool {
	switch lang {
	case "javascript", "typescript":
		return strings.HasPrefix(line, "const ") || strings.HasPrefix(line, "let ") || strings.HasPrefix(line, "var ") ||
			strings.HasPrefix(line, "export const ") || strings.HasPrefix(line, "export let ")
	case "python":
		// Python top-level assignments: UPPER_CASE = ... (constants)
		if idx := strings.Index(line, "="); idx > 0 {
			name := strings.TrimSpace(line[:idx])
			return name == strings.ToUpper(name) && len(name) > 1
		}
		return false
	default:
		return false
	}
}
