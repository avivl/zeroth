# Fixture sizes

Recorded by `fixtures/build.sh`. Tars are uncompressed (`tar -cf`) so later
gates can measure compression. Fixture M is a real Go clone plus its module
cache, not synthetic files.

GitHub rejects blobs over 100 MB and Git LFS caps a file at 2 GB, so only
**S.tar** is committed. Recreate M and L locally with `./fixtures/build.sh`.

| Size | Tar | Tar bytes | Unpacked bytes | Contents |
| --- | --- | ---: | ---: | --- |
| S | `zeroth-spike/fixtures/S.tar` | 10516480 | 10485884 | scripts |
| M | `zeroth-spike/fixtures/M.tar` | 557199360 | 524291722 | prometheus clone + GOMODCACHE |
| L | `zeroth-spike/fixtures/L.tar` | 5401610240 | 5368709120 | M plus binary assets |
