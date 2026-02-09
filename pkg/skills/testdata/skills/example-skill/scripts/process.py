#!/usr/bin/env python3
import sys

def main():
    if len(sys.argv) < 3:
        print("Usage: process.py <input_file> <output_file> [--uppercase] [--count-words]")
        sys.exit(1)
    
    input_file = sys.argv[1]
    output_file = sys.argv[2]
    uppercase = "--uppercase" in sys.argv
    count_words = "--count-words" in sys.argv
    
    # Read input
    with open(input_file, 'r') as f:
        text = f.read()
    
    # Process
    if uppercase:
        text = text.upper()
    
    result = text
    if count_words:
        word_count = len(text.split())
        result = f"Word count: {word_count}\n\n{text}"
    
    # Write output
    with open(output_file, 'w') as f:
        f.write(result)
    
    print(f"Processed {input_file} -> {output_file}")
    if count_words:
        print(f"Word count: {len(text.split())}")

if __name__ == "__main__":
    main()
