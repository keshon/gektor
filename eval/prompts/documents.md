# Native document readers

The fixture contains a small DOCX brief, an XLSX sheet and a text PDF.
The expected values are Cedar Works, INVOICE-73, 1847, the saved formula result
3694, and ORBIT-52. These are synthetic files with no external data.

`33-documents-read` checks discovery and use of all three native readers.
It forbids file edits, archive extraction and command execution. The final
answer must contain the values actually read.

`34-documents-workflow` checks the same readers as recipe nodes. It requires
schema discovery, validation, execution and run inspection. The saved recipe
must name all three readers; the answer must contain the fixture values.

Go tests cover exact parsing, paging, output limits, malformed input, permissions
and worker failure. These probes check model decisions. A dry run validates
probe definitions; it does not establish that a model passes.
