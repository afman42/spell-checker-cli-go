# Spell Checker Cli

for raw text, not compile or binary

usage cli:

- file:
  `./spellchecker <file>`
- dir:
  `./spellchecker <directory>`

```bash
Usage of ./spellchecker:
  -dict string
    	Optional: path to a custom CSV dictionary file.
  -exclude string
    	Optional: comma-separated list of file patterns to exclude.
  -format string
    	Optional: output format (txt, html). Overrides filename extension.
  -output string
    	Optional: path to an output file.
```

```bash
# Run it on the directory, excluding .log and .tmp files
./spellchecker -dict "my_dict.csv" -exclude "*.log,*.tmp" -output my_archive.html ./my_project

# Run it on the directory, excluding .log and .tmp files
./spellchecker -exclude "*.log,*.tmp" ./my_project

# This correctly generates a TEXT report, ignoring "html" in the name
./spellchecker -output my-html-notes.txt my_document.txt

# This correctly generates an HTML report
./spellchecker -output my_archive.html my_document.txt
```

example file `my_dict.csv`:

```bash
word,pos,def
A,,"The first letter of the English and of many other alphabets. The capital A of the alphabets of Middle and Western Europe, as also the small letter (a), besides the forms in Italic, black letter, etc., are all descended from the old Latin A, which was borrowed from the Greek Alpha, of the same form; and this was made from the first letter (/) of the Phoenician alphabet, the equivalent of the Hebrew Aleph, and itself from the Egyptian origin. The Aleph was a consonant letter, with a guttural breath sound that was not an element of Greek articulation; and the Greeks took it to represent their vowel Alpha with the a sound, the Phoenician alphabet having no vowel symbols."
A,,"The name of the sixth tone in the model major scale (that in C), or the first tone of the minor scale, which is named after it the scale in A minor. The second string of the violin is tuned to the A in the treble staff. -- A sharp (A/) is the name of a musical tone intermediate between A and B. -- A flat (A/) is the name of a tone intermediate between A and G."
```

for testing in folder `test`
