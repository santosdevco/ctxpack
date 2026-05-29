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
	"sort"
	"strings"
)

// --- Estructuras para el JSON ---
type Config struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// --- Estructuras para el XML ---
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

func main() {
	// Flags de configuración
	configFile := flag.String("config", "config.json", "Ruta al archivo JSON de configuración")
	outputFile := flag.String("out", "context.xml", "Ruta del archivo XML de salida")
	baseDir := flag.String("base", ".", "Directorio base de los proyectos")
	addLines := flag.Bool("lines", true, "Agregar números de línea al código")
	stripEmpty := flag.Bool("strip-empty", true, "Eliminar líneas vacías para ahorrar tokens")

	flag.Parse()

	// 1. Leer JSON de configuración
	jsonFile, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Error leyendo JSON: %v", err)
	}

	var config Config
	if err := json.Unmarshal(jsonFile, &config); err != nil {
		log.Fatalf("Error parseando JSON: %v", err)
	}

	var codebase XMLCodebase
	processedFiles := make(map[string]bool)

	// 2. Procesar inclusiones y exclusiones
	for _, incPattern := range config.Include {
		searchPattern := filepath.Join(*baseDir, incPattern)
		matches, err := filepath.Glob(searchPattern)

		if err != nil {
			log.Printf("Advertencia: Patrón inválido '%s': %v\n", searchPattern, err)
			continue
		}

		for _, match := range matches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			// Si es un directorio, buscar recursivamente
			if fileInfo.IsDir() {
				filepath.WalkDir(match, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}

					// Filtro duro: Ignorar carpetas basura masivas automáticamente
					if d.IsDir() {
						name := d.Name()
						if name == "node_modules" || name == ".git" || name == "dist" || name == "build" || name == "__pycache__" || name == ".next" || name == "coverage" {
							return filepath.SkipDir
						}
						return nil
					}

					// Archivos procesados o excluidos por el JSON
					if processedFiles[path] || isExcluded(path, *baseDir, config.Exclude) {
						return nil
					}

					addFile(path, *baseDir, &codebase, processedFiles, *addLines, *stripEmpty)
					return nil
				})
				continue
			}

			// Si es un archivo normal (y no ha sido procesado ni excluido)
			if !processedFiles[match] && !isExcluded(match, *baseDir, config.Exclude) {
				addFile(match, *baseDir, &codebase, processedFiles, *addLines, *stripEmpty)
			}
		}
	}

	// 3. Generar el Árbol de Directorios (Solo de lo procesado)
	var paths []string
	for _, f := range codebase.Files {
		paths = append(paths, f.Path)
	}
	sort.Strings(paths) // Orden alfabético para que sea fácil de leer por la IA

	var treeBuilder strings.Builder
	treeBuilder.WriteString("\n")
	for _, p := range paths {
		treeBuilder.WriteString(fmt.Sprintf("  - %s\n", p))
	}
	codebase.DirectoryTree = treeBuilder.String()

	// 4. Inyectar Metadatos e Instrucciones (Estilo Repomix)
	codebase.Summary = FileSummary{
		Purpose:    "Este archivo contiene una representación empaquetada de componentes específicos del repositorio. Diseñado para ser consumido por IA para revisión de código, refactorización y auditoría de seguridad.",
		FileFormat: "1. Este resumen (file_summary)\n2. Estructura de archivos incluidos (directory_structure)\n3. Archivos fuente encapsulados en CDATA.",
		Usage:      "Lee este archivo como solo-lectura. Indica los cambios referenciando la ruta exacta y los números de línea. Evita reescribir archivos masivos enteros; proporciona solo los bloques modificados si es posible.",
		Notes:      "Las líneas completamente vacías fueron removidas para optimizar tokens, pero la numeración original se mantiene exacta para referencias precisas. Filtros automáticos descartaron node_modules, .git y compilados.",
	}

	// 5. Generar XML final
	xmlData, err := xml.MarshalIndent(codebase, "", "  ")
	if err != nil {
		log.Fatalf("Error generando XML: %v", err)
	}

	finalXML := []byte(xml.Header + string(xmlData))
	if err := os.WriteFile(*outputFile, finalXML, 0644); err != nil {
		log.Fatalf("Error guardando salida: %v", err)
	}

	fmt.Printf("¡Empaquetado exitoso! Archivos procesados: %d. Contexto guardado en: %s\n", len(codebase.Files), *outputFile)
}

// Función auxiliar para agregar archivos
func addFile(filePath, baseDir string, codebase *XMLCodebase, processedFiles map[string]bool, addLines, stripEmpty bool) {
	content, err := processFile(filePath, addLines, stripEmpty)
	if err != nil {
		log.Printf("Error leyendo %s: %v\n", filePath, err)
		return
	}

	relPath, _ := filepath.Rel(baseDir, filePath)
	codebase.Files = append(codebase.Files, XMLFile{
		Path:    relPath,
		Content: content,
	})
	processedFiles[filePath] = true
}

// Verifica exclusiones personalizadas del config.json
func isExcluded(filePath, baseDir string, excludes []string) bool {
	for _, excPattern := range excludes {
		excSearch := filepath.Join(baseDir, excPattern)
		matched, _ := filepath.Match(excSearch, filePath)
		baseMatched, _ := filepath.Match(excPattern, filepath.Base(filePath))

		if matched || baseMatched {
			return true
		}
	}
	return false
}

// Procesa el archivo, limpia líneas vacías y añade numeración
func processFile(filePath string, addLines bool, stripEmpty bool) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var builder strings.Builder
	scanner := bufio.NewScanner(file)
	lineNum := 1

	builder.WriteString("\n")

	for scanner.Scan() {
		line := scanner.Text()

		if stripEmpty && strings.TrimSpace(line) == "" {
			lineNum++
			continue
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
