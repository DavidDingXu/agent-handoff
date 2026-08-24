# Third-Party Notices

agent-handoff binaries include open-source Go dependencies. Their license texts are distributed in [`third_party_licenses/`](third_party_licenses/):

| Dependency | License |
| --- | --- |
| `github.com/dustin/go-humanize` | MIT |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/mattn/go-isatty` | MIT |
| `github.com/ncruces/go-strftime` | MIT |
| `github.com/remyoudompheng/bigfft` | BSD-3-Clause |
| `golang.org/x/sys/unix` | BSD-3-Clause |
| `modernc.org/libc` and its bundled notices | MIT and listed third-party licenses |
| `modernc.org/mathutil` | BSD-3-Clause |
| `modernc.org/memory` | BSD-3-Clause |
| `modernc.org/sqlite` | BSD-3-Clause |

This inventory covers packages linked into the release binary, generated from the Go module graph with `google/go-licenses` and supplemented with the upstream `modernc.org/mathutil` license that the classifier does not recognize automatically.
