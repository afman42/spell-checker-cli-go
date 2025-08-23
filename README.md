# Spell Checker Cli

for raw text, not compile or binary

usage cli:

- file:
  `./spellchecker <file>`
- dir:
  `./spellchecker <directory>`

```bash
Usage of ./spellchecker:
  --dict string
    	Optional: path to a custom CSV dictionary file.
  --exclude string
    	Optional: comma-separated list of file patterns to exclude.
  --format string
    	Optional: output format (txt, html). Overrides filename extension.
  --output string
    	Optional: path to an output file or directory (for HTML reports).
  --personal-dict string
    	Optional: path to a personal dictionary file (one word per line).
  --verbose
    	Enable verbose logging to show skipped files and directories.
```

```bash
# Run it on the directory, excluding .log and .tmp files and add custom dictionary
./spellchecker --dict "my_dict.csv" --exclude "*.log,*.tmp" --output my_arcive.txt --verbose ./my_project

# Run it on the directory, excluding .log and .tmp files
./spellchecker --dict "my_dict.csv" --exclude "*.log,*.tmp" --output ./report-html/ --format html ./my_project

# Run it on the directory, excluding .log and .tmp files
./spellchecker --exclude "*.log,*.tmp" ./my_project

# This correctly generates a TEXT report, ignoring "html" in the name
./spellchecker --output my-html-notes.txt my_document.txt

# Run check verbose file
./spellchecker --verbose my_document.txt

# Run it on the directory, and add file personal dictionary
./spellchecker --personal-dict ./personal-dict.txt --verbose my_document.txt

# Run it on the directory, and add file personal dictionary, custom file dictionary without file emmbed data
./spellchecker --dict "my_dict.csv" --personal-dict ./personal-dict.txt --verbose my_document.txt
```

another option, add configuration file:

- `spellchecker.yaml`

```yaml
# A list of glob patterns to exclude from the scan.
exclude:
  - "*.log"
  - "build/"
  - "vendor/"

# Path to a personal word list to add to the dictionary.
personal-dictionary: ".project-words.txt"

# Default output format and path.
format: "html"
output: "./spellcheck-reports/"
```

- or another option file `spellchecker.json`

```json
{
  "exclude": ["*.log", "build/", "vendor/"],
  "personal-dictionary": ".project-words.txt",
  "format": "html",
  "output": "./spellcheck-reports/"
}
```

```bash
# Run it on the directory, and add file configuration custom, flag verbose
./spellchecker --verbose <directory>

# Run it on the directory, and add file configuration custom
./spellchecker <directory>

# This will generate a text report to the terminal, overriding the
# "output" and "format" settings in the config file for this one run.
./spell-checker-cli --output "" <directory>
```

example file `my_dict.csv` :

```bash
word,pos,def
A,,"The first letter of the English and of many other alphabets. The capital A of the alphabets of Middle and Western Europe, as also the small letter (a), besides the forms in Italic, black letter, etc., are all descended from the old Latin A, which was borrowed from the Greek Alpha, of the same form; and this was made from the first letter (/) of the Phoenician alphabet, the equivalent of the Hebrew Aleph, and itself from the Egyptian origin. The Aleph was a consonant letter, with a guttural breath sound that was not an element of Greek articulation; and the Greeks took it to represent their vowel Alpha with the a sound, the Phoenician alphabet having no vowel symbols."
A,,"The name of the sixth tone in the model major scale (that in C), or the first tone of the minor scale, which is named after it the scale in A minor. The second string of the violin is tuned to the A in the treble staff. -- A sharp (A/) is the name of a musical tone intermediate between A and B. -- A flat (A/) is the name of a tone intermediate between A and G."
```

for testing in folder `test`

The new regular expression `[a-zA-Z']+(?:-[a-zA-Z']+)*` is more sophisticated:

- `[a-zA-Z']+`: This is the first part, which matches a standard word or contraction (e.g., "state").
- `(?: ... )*`: This is the second part. The \* means it will match the pattern inside the parentheses zero or more times. This allows it to correctly identify non-hyphenated words too. The ?: makes it a non-capturing group for efficiency.
- `-[a-zA-Z']+`: This is the pattern inside the group. It looks for a hyphen followed by another word segment (e.g., "-of", "-the", "-art").
  Together, this regex perfectly matches "state-of-the-art", "don't", and "word" as single, complete tokens.

for example `personal-dict.txt`:

```
Qopper
FluxCapacitor
# This is a comment
bigcorp-api
Gregor
Samsa
```
