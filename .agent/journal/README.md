# Journal — append-only history. Written at every session END; read only to investigate the past.
# Grep it — never load whole files into context. Monthly files (<YYYY-MM>.md), created on first write.
# maintain.md rolls months older than 2 into ARCHIVE.md (one line per session, live flags kept).

Entry format (≤4 lines per session):
S<n> <YYYY-MM-DD> <workflow>: outcome + key files touched [refs: <design>/D<n>]
  flag: <debt, risk, or cleanup noticed but out of scope — optional; maintain.md promotes these
        to ISSUES.md or drops them>
