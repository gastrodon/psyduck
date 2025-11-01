#!/bin/bash
# Generate call graph visualization for the parser package
# This script performs static analysis on the parse package and generates a PNG image

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Call Graph Generator for psyduck parser ===${NC}"

# Get the directory of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT_DIR="${PROJECT_ROOT}/parse"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

# Check if Graphviz is installed
if ! command -v dot &> /dev/null; then
    echo -e "${RED}Error: Graphviz is not installed.${NC}"
    echo -e "${BLUE}Please install Graphviz:${NC}"
    echo "  Ubuntu/Debian: sudo apt-get install graphviz"
    echo "  macOS:         brew install graphviz"
    echo "  Other:         https://graphviz.org/download/"
    exit 1
fi

# Install callgraph tool if not already installed
if ! command -v callgraph &> /dev/null; then
    echo -e "${BLUE}Installing Go callgraph tool...${NC}"
    go install golang.org/x/tools/cmd/callgraph@latest
fi

# Navigate to project root
cd "${PROJECT_ROOT}"

echo -e "${BLUE}Analyzing parse package...${NC}"

# Create temporary files securely
TEMP_RAW=$(mktemp /tmp/callgraph_raw.XXXXXX)
TEMP_DOT=$(mktemp /tmp/callgraph.XXXXXX.dot)

# Cleanup function
cleanup() {
    rm -f "$TEMP_RAW" "$TEMP_DOT"
}
trap cleanup EXIT

# Generate call graph data using static analysis
# Filter only for parse package functions to reduce noise
callgraph -algo=static ./parse 2>&1 | \
    grep "github.com/gastrodon/psyduck/parse\." | \
    grep -v "\.init" > "$TEMP_RAW" || true

# Convert to DOT format
echo -e "${BLUE}Generating DOT file...${NC}"

cat > "$TEMP_DOT" <<'EOF'
digraph callgraph {
    rankdir=LR;
    node [shape=box, style=rounded, fontname="Arial"];
    edge [color="#555555"];
    
    // Graph attributes
    graph [fontname="Arial", fontsize=12, label="Parser Call Graph\n\n", labelloc=t];
    
EOF

# Process the call graph data
# Note: External dependencies (fmt, os, yaml, plugin, filepath, exec) are shown 
# to provide context for key integrations without cluttering the graph
awk -F '\t' '
{
    # Extract caller and callee from the line
    caller = $1
    callee = $3
    
    # Clean up function names (remove package prefix for readability)
    gsub(/github\.com\/gastrodon\/psyduck\/parse\./, "", caller)
    gsub(/\(/, "", caller)
    gsub(/\)\./, ".", caller)
    gsub(/\*/, "", caller)
    
    # Only keep external calls that are relevant
    if (callee ~ /github\.com\/gastrodon\/psyduck\/parse\./) {
        gsub(/github\.com\/gastrodon\/psyduck\/parse\./, "", callee)
        gsub(/\(/, "", callee)
        gsub(/\)\./, ".", callee)
        gsub(/\*/, "", callee)
        print "    \"" caller "\" -> \"" callee "\" [color=\"#0066cc\"];"
    } else if (callee ~ /(fmt|os|yaml|plugin|filepath|exec)/) {
        # Show important external dependencies with different color
        gsub(/.*\//, "", callee)
        gsub(/\(/, "", callee)
        gsub(/\)\./, ".", callee)
        gsub(/\*/, "", callee)
        print "    \"" caller "\" -> \"" callee "\" [color=\"#999999\", style=dashed];"
    }
}
' "$TEMP_RAW" | sort -u >> "$TEMP_DOT"

cat >> "$TEMP_DOT" <<'EOF'
    
    // Define node styles for key public API functions
    // These are highlighted as they are the main entry points
    "ParseFile" [fillcolor="#e6f3ff", style="rounded,filled"];
    "ParseDir" [fillcolor="#e6f3ff", style="rounded,filled"];
    "ParseString" [fillcolor="#e6f3ff", style="rounded,filled"];
    "PluginDesc.Load" [fillcolor="#ffe6e6", style="rounded,filled"];
    "PluginDesc.Fetch" [fillcolor="#ffe6e6", style="rounded,filled"];
}
EOF

echo -e "${BLUE}Rendering PNG image...${NC}"

# Generate PNG from DOT file
dot -Tpng "$TEMP_DOT" -o "${OUTPUT_DIR}/callgraph.png"

echo -e "${GREEN}✓ Call graph generated successfully: ${OUTPUT_DIR}/callgraph.png${NC}"
echo -e "${BLUE}Image size: $(du -h "${OUTPUT_DIR}/callgraph.png" | cut -f1)${NC}"
