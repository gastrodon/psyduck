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
    echo -e "${BLUE}Installing Graphviz...${NC}"
    if command -v apt-get &> /dev/null; then
        sudo apt-get update && sudo apt-get install -y graphviz
    elif command -v brew &> /dev/null; then
        brew install graphviz
    else
        echo -e "${RED}Error: Could not install Graphviz. Please install it manually.${NC}"
        exit 1
    fi
fi

# Install callgraph tool if not already installed
if ! command -v callgraph &> /dev/null; then
    echo -e "${BLUE}Installing Go callgraph tool...${NC}"
    go install golang.org/x/tools/cmd/callgraph@latest
fi

# Navigate to project root
cd "${PROJECT_ROOT}"

echo -e "${BLUE}Analyzing parse package...${NC}"

# Generate call graph data using static analysis
# Filter only for parse package functions to reduce noise
callgraph -algo=static ./parse 2>&1 | \
    grep "github.com/gastrodon/psyduck/parse\." | \
    grep -v "\.init" > /tmp/callgraph_raw.txt || true

# Convert to DOT format
echo -e "${BLUE}Generating DOT file...${NC}"

cat > /tmp/callgraph.dot <<'EOF'
digraph callgraph {
    rankdir=LR;
    node [shape=box, style=rounded, fontname="Arial"];
    edge [color="#555555"];
    
    // Graph attributes
    graph [fontname="Arial", fontsize=12, label="Parser Call Graph\n\n", labelloc=t];
    
EOF

# Process the call graph data
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
' /tmp/callgraph_raw.txt | sort -u >> /tmp/callgraph.dot

cat >> /tmp/callgraph.dot <<'EOF'
    
    // Define node styles for key functions
    "ParseFile" [fillcolor="#e6f3ff", style="rounded,filled"];
    "ParseDir" [fillcolor="#e6f3ff", style="rounded,filled"];
    "ParseString" [fillcolor="#e6f3ff", style="rounded,filled"];
    "PluginDesc.Load" [fillcolor="#ffe6e6", style="rounded,filled"];
    "PluginDesc.Fetch" [fillcolor="#ffe6e6", style="rounded,filled"];
}
EOF

echo -e "${BLUE}Rendering PNG image...${NC}"

# Generate PNG from DOT file
dot -Tpng /tmp/callgraph.dot -o "${OUTPUT_DIR}/callgraph.png"

# Clean up temporary files
rm -f /tmp/callgraph.dot /tmp/callgraph_raw.txt

echo -e "${GREEN}✓ Call graph generated successfully: ${OUTPUT_DIR}/callgraph.png${NC}"
echo -e "${BLUE}Image size: $(du -h "${OUTPUT_DIR}/callgraph.png" | cut -f1)${NC}"
