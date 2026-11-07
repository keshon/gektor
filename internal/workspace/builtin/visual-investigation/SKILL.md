---
name: visual-investigation
description: Identify pictured objects, compare visual references, or inspect website appearance with image and browser tools.
---

# Visual investigation

Enable `native_images`. `image_attachments` lists pictures already in this
session, including clipboard input and browser screenshots. Pass an ID such as
`attachment:1` to `image_view`, `image_inspect`, `image_compare` or other image
tools. IDs belong to this conversation; reusable recipes need workspace files.
Sources stay unchanged. `image_variants` writes only to its chosen new directory.

Look at the supplied picture before choosing search terms. Separate visible
features from guesses about function, origin or exact model. To inspect a small
detail, use `image_view` with `crop` (x, y, width, height in original pixels)
and optional display `width`. Use the original dimensions from the attachment
list or a full view when choosing coordinates. Enlarging does not recover missing detail.

Search for a reference, then view it with `image_download(url)` or a browser
screenshot. Keep its URL with the finding. Compare distinguishing features;
look for contradictions as well as similarities. A matching caption supports
a hypothesis but does not verify every visible detail. Stop when the evidence
answers the question; report what is still uncertain.

`image_compare` measures pixel fidelity for aligned images, not object identity.
Different views, lighting, dimensions or backgrounds make those scores poor
identification evidence. Do not use a low SSIM as proof of different objects.

For browser work, use `browser_observe` text for focused reading, controls for
current references, and visual for appearance. Refresh references after
navigation or a stale-ref error. Verify the requested interaction, including
mobile menu behavior if relevant; resizing alone is not a functional test.
An empty initial snapshot may need another observation after content loads.
Report relevant console/network failures; a third-party warning alone does
not establish a site defect.

Reverse-image search and OCR are not native tools. Do not invent them or claim
an exact source, authorship or measurement from visual resemblance alone.
