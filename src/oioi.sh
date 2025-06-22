#!/bin/bash

OUTPUT_FILE="${1:-go_files_combined.txt}"

> "$OUTPUT_FILE"

for file in $(find . -name "*.go" | sort); do
    echo "====== $file ======" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"
    cat "$file" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"
done

echo "Done! Output in $OUTPUT_FILE"