package main

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
)

// --- Types ---

type Range struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Context string `json:"context,omitempty"`
}

// IncludeTarget acepta string simple o objeto con rangos en el JSON.
// Ejemplos válidos:
//   "src/**/*.go"
//   {"path": "main.go", "ranges": [{"start": 10, "end": 80}]}
type IncludeTarget struct {
	Path   string  `json:"path"`
	Ranges []Range `json:"ranges,omitempty"`
}

func (t *IncludeTarget) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		t.Path = s
		return nil
	}
	type Alias IncludeTarget
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = IncludeTarget(a)
	return nil
}

type Config struct {
	Include []IncludeTarget `json:"include"`
	Exclude []string        `json:"exclude"`
}

// --- XML structures ---

type XMLFile struct {
	Path    string `xml:"path,attr"`
	Content string `xml:",cdata"`
}

type FileSummary struct {
	Purpose    string `xml:"purpose"`
	FileFormat string `xml:"file_format"`
	Usage      string `xml:"usage_guidelines"`
	Notes      string `xml:"notes"`
}

type XMLCodebase struct {
	XMLName       xml.Name    `xml:"repository"`
	Summary       FileSummary `xml:"file_summary"`
	DirectoryTree string      `xml:"directory_structure"`
	Files         []XMLFile   `xml:"file"`
}

// --- Default ignores ---

var defaultIgnoreDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".next":        true,
	"coverage":     true,
	"vendor":       true,
}

// dangerousPatterns: archivos que nunca deben incluirse en el output.
var dangerousPatterns = []string{
	".env", ".env.local", ".env.production", ".env.staging", ".env.development",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.crt",
	"id_rsa", "id_ed25519", "id_dsa", "id_ecdsa",
	"credentials.json", "secrets.json", "serviceAccountKey.json",
	".netrc", ".npmrc",
}

// --- Secret masking ---

var secretRegexes = []*regexp.Regexp{
	// key=value o key: value (API keys, tokens, passwords, etc.)
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|pwd|auth|bearer|credential)\s*[=:]\s*\S+`),
	// Tokens de proveedores conocidos (GitHub, OpenAI, AWS)
	regexp.MustCompile(`(ghp_|gho_|ghs_|sk-|AKIA)[A-Za-z0-9_/+=]{10,}`),
	// URLs con credenciales embebidas
	regexp.MustCompile(`https?://[^:\s]+:[^@\s]+@\S+`),
}

func maskSecrets(line string) string {
	result := line
	for _, re := range secretRegexes {
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			eqIdx := strings.IndexAny(match, "=:")
			if eqIdx > 0 {
				return match[:eqIdx+1] + " [MASKED]"
			}
			return "[MASKED]"
		})
	}
	return result
}

// --- .gitignore support ---

func loadGitignorePatterns(baseDir string) []string {
	file, err := os.Open(filepath.Join(baseDir, ".gitignore"))
	if err != nil {
		return nil
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		patterns = append(patterns, strings.TrimSuffix(line, "/"))
	}
	return patterns
}

// --- Dangerous file check ---

func isDangerous(filename string) bool {
	for _, pattern := range dangerousPatterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
		if filename == pattern {
			return true
		}
	}
	return false
}

// --- Main ---

func main() {
	configFile := flag.String("config", "", "Archivo JSON de configuración (opcional si se usa --path)")
	pathFlag := flag.String("path", "", "Directorio a empaquetar directamente, sin config JSON")
	outputFile := flag.String("out", "context.xml", "Archivo de salida")
	baseDir := flag.String("base", ".", "Directorio base para rutas relativas")
	addLines := flag.Bool("lines", true, "Agregar números de línea al código")
	stripEmpty := flag.Bool("strip-empty", true, "Eliminar líneas vacías para ahorrar tokens")
	doMask := flag.Bool("mask-secrets", false, "Enmascarar secretos detectados con [MASKED]")
	respectGitignore := flag.Bool("respect-gitignore", true, "Respetar patrones del .gitignore")
	noDefaultIgnore := flag.Bool("no-default-ignore", false, "Desactivar filtros automáticos (node_modules, .git, dist...)")
	format := flag.String("format", "xml", "Formato de salida: xml o md")
	copyPrompt := flag.Bool("copy-prompt", false, "Print the AI chat context message and copy it to clipboard")

	flag.Parse()

	// Si se usa --path, ese directorio pasa a ser la base
	if *pathFlag != "" && *baseDir == "." {
		*baseDir = *pathFlag
	}

	var gitignorePatterns []string
	if *respectGitignore {
		gitignorePatterns = loadGitignorePatterns(*baseDir)
	}

	var config Config

	if *pathFlag != "" {
		config.Include = []IncludeTarget{{Path: "."}}
	} else {
		cfgPath := *configFile
		if cfgPath == "" {
			cfgPath = "config.json"
		}
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			log.Fatalf("Error leyendo config: %v\n  Usa --path <dir> para empaquetar sin config JSON.", err)
		}
		if err := json.Unmarshal(data, &config); err != nil {
			log.Fatalf("Error parseando config: %v", err)
		}
	}

	var codebase XMLCodebase
	processedFiles := make(map[string]bool)

	for _, target := range config.Include {
		searchPattern := filepath.Join(*baseDir, target.Path)
		matches, err := filepath.Glob(searchPattern)
		if err != nil {
			log.Printf("Advertencia: Patrón inválido '%s': %v\n", searchPattern, err)
			continue
		}
		// Glob no matchea paths literales que no existen como patrón; fallback directo
		if len(matches) == 0 {
			if _, err := os.Stat(searchPattern); err == nil {
				matches = []string{searchPattern}
			} else {
				log.Printf("Warning: '%s' not found (from include config)\n", target.Path)
			}
		}

		for _, match := range matches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			if fileInfo.IsDir() {
				filepath.WalkDir(match, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() {
						if !*noDefaultIgnore && defaultIgnoreDirs[d.Name()] {
							return filepath.SkipDir
						}
						return nil
					}
					if processedFiles[path] || isExcluded(path, *baseDir, config.Exclude, gitignorePatterns) {
						return nil
					}
					addFile(path, *baseDir, &codebase, processedFiles, *addLines, *stripEmpty, nil, *doMask)
					return nil
				})
				continue
			}

			if !processedFiles[match] && !isExcluded(match, *baseDir, config.Exclude, gitignorePatterns) {
				addFile(match, *baseDir, &codebase, processedFiles, *addLines, *stripEmpty, target.Ranges, *doMask)
			}
		}
	}

	// Árbol de directorios
	var paths []string
	for _, f := range codebase.Files {
		paths = append(paths, f.Path)
	}
	sort.Strings(paths)

	var treeBuilder strings.Builder
	treeBuilder.WriteString("\n")
	for _, p := range paths {
		treeBuilder.WriteString(fmt.Sprintf("  - %s\n", p))
	}
	codebase.DirectoryTree = treeBuilder.String()

	// Metadata
	notes := "Empty lines removed to save tokens."
	if !*noDefaultIgnore {
		notes += " Auto-ignored: node_modules, .git, dist, build, vendor."
	}
	if *doMask {
		notes += " Detected secrets masked with [MASKED]."
	}
	if len(gitignorePatterns) > 0 {
		notes += fmt.Sprintf(" Applied %d .gitignore patterns.", len(gitignorePatterns))
	}

	codebase.Summary = FileSummary{
		Purpose:    "Packed repository snapshot for AI consumption: code review, surgical refactoring, and security auditing.",
		FileFormat: "1. file_summary  2. directory_structure  3. Source files wrapped in CDATA.",
		Usage:      "Read-only. Reference changes by exact file path and line number. Line number gaps indicate surgical range extraction.",
		Notes:      notes,
	}

	// Generar output según formato
	switch *format {
	case "md":
		content := renderMarkdown(codebase)
		if err := os.WriteFile(*outputFile, []byte(content), 0644); err != nil {
			log.Fatalf("Error guardando salida: %v", err)
		}
	default:
		xmlData, err := xml.MarshalIndent(codebase, "", "  ")
		if err != nil {
			log.Fatalf("Error generando XML: %v", err)
		}
		if err := os.WriteFile(*outputFile, []byte(xml.Header+string(xmlData)), 0644); err != nil {
			log.Fatalf("Error guardando salida: %v", err)
		}
	}

	fmt.Printf("Packed successfully. Files: %d → %s\n", len(codebase.Files), *outputFile)

	if *copyPrompt {
		outName := filepath.Base(*outputFile)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("@%s contains the verbatim source code of these files, with exact line numbers:\n", outName))
		for _, p := range paths {
			sb.WriteString(fmt.Sprintf("  - %s\n", p))
		}
		sb.WriteString("\nThe XML is identical to what is on disk — use it as your working source, not as a reference.\nDo not use file reading tools. Everything you need is already inside the XML.\nFor any change, cite the exact file path and line number from the XML.\n")
		msg := sb.String()
		fmt.Printf("\n--- copy this to the start of your AI chat ---\n%s----------------------------------------------\n", msg)
		if err := clipboard.WriteAll(msg); err != nil {
			fmt.Printf("(clipboard unavailable: %v)\n", err)
		} else {
			fmt.Println("(copied to clipboard)")
		}
	}
}

// --- Helpers ---

func isBinaryFile(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

func addFile(filePath, baseDir string, codebase *XMLCodebase, processedFiles map[string]bool, addLines, stripEmpty bool, ranges []Range, doMask bool) {
	if isBinaryFile(filePath) {
		return
	}
	content, err := processFile(filePath, addLines, stripEmpty, ranges, doMask)
	if err != nil {
		log.Printf("Error leyendo %s: %v\n", filePath, err)
		return
	}
	relPath, _ := filepath.Rel(baseDir, filePath)
	codebase.Files = append(codebase.Files, XMLFile{Path: filepath.ToSlash(relPath), Content: content})
	processedFiles[filePath] = true
}

func isExcluded(filePath, baseDir string, excludes []string, gitignorePatterns []string) bool {
	baseName := filepath.Base(filePath)
	relPath, _ := filepath.Rel(baseDir, filePath)

	// Archivos peligrosos: siempre excluidos
	if isDangerous(baseName) {
		return true
	}

	// Exclusiones del config JSON
	for _, excPattern := range excludes {
		if matched, _ := filepath.Match(filepath.Join(baseDir, excPattern), filePath); matched {
			return true
		}
		if matched, _ := filepath.Match(excPattern, baseName); matched {
			return true
		}
	}

	// Patrones de .gitignore
	for _, pattern := range gitignorePatterns {
		if matched, _ := filepath.Match(pattern, baseName); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return true
		}
	}

	return false
}

func processFile(filePath string, addLines bool, stripEmpty bool, ranges []Range, doMask bool) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var builder strings.Builder
	scanner := bufio.NewScanner(file)
	// Buffer generoso para archivos con líneas muy largas (ej. minificados)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)
	lineNum := 1
	hasRanges := len(ranges) > 0

	builder.WriteString("\n")

	for scanner.Scan() {
		line := scanner.Text()

		if stripEmpty && strings.TrimSpace(line) == "" {
			lineNum++
			continue
		}

		if hasRanges {
			inRange := false
			for _, r := range ranges {
				if lineNum >= r.Start && lineNum <= r.End {
					inRange = true
					break
				}
			}
			if !inRange {
				lineNum++
				continue
			}
		}

		if doMask {
			line = maskSecrets(line)
		}

		if addLines {
			builder.WriteString(fmt.Sprintf("%d: %s\n", lineNum, line))
		} else {
			builder.WriteString(line + "\n")
		}
		lineNum++
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return builder.String(), nil
}

func renderMarkdown(codebase XMLCodebase) string {
	var sb strings.Builder
	sb.WriteString("# Code Context\n\n")
	sb.WriteString("## Directory Structure\n\n```\n")
	sb.WriteString(codebase.DirectoryTree)
	sb.WriteString("```\n\n")

	for _, f := range codebase.Files {
		lang := extToLang(filepath.Ext(f.Path))
		sb.WriteString(fmt.Sprintf("## %s\n\n```%s\n", f.Path, lang))
		sb.WriteString(f.Content)
		sb.WriteString("```\n\n")
	}
	return sb.String()
}

func extToLang(ext string) string {
	langs := map[string]string{
		".go": "go", ".js": "javascript", ".ts": "typescript", ".tsx": "typescript",
		".jsx": "javascript", ".py": "python", ".rs": "rust", ".java": "java",
		".c": "c", ".cpp": "cpp", ".h": "c", ".cs": "csharp",
		".css": "css", ".scss": "scss", ".html": "html", ".json": "json",
		".yaml": "yaml", ".yml": "yaml", ".md": "markdown", ".sh": "bash",
		".sql": "sql", ".rb": "ruby", ".php": "php", ".swift": "swift",
		".kt": "kotlin", ".vue": "vue", ".svelte": "svelte", ".lua": "lua",
		".toml": "toml", ".xml": "xml", ".tf": "hcl",
	}
	if lang, ok := langs[strings.ToLower(ext)]; ok {
		return lang
	}
	return ""
}
