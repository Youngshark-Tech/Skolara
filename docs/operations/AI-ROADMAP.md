# AI Roadmap — The Intelligence Layer

Skolara's positioning is **the Intelligent Operating System for Schools**,
and its product loop ends in *Learn*. ADR-010 fixed the constitution before
any AI code exists: a provider-agnostic AI gateway where **intelligence
recommends, deterministic domains decide, and authorized humans approve**.
This roadmap turns the hardened domain foundation into demonstrable,
monetizable intelligence.

**Epic:** [#184](https://github.com/Youngshark-Tech/Skolara/issues/184)

## Why this wins (product + investor view)

| Lever | Features | What it proves |
|-------|----------|----------------|
| Retention moat | Ask Skolara (#160), WhatsApp concierge (#167) | Daily active usage instead of term-end reporting |
| Provable ROI | Fee-risk + smart reminders (#162), fee templates (#179) | Faster fee collection, measured in-product |
| Low-touch onboarding (lower CAC) | Document-intelligence intake (#164), onboarding wizard (#170) | Shorter sales cycles; schools migrate from paper in days |
| Outcome stories | Early-warning signals (#163), grading assist (#165), narratives (#161) | Better learner outcomes — what boards actually buy |
| Defensibility | All of the above | Grounded in tenant-scoped, cited, audit-logged school data — generic AI wrappers cannot cross the data boundary |

## The feature ladder (build order)

1. **Ask Skolara** (#160, P1) — natural-language questions over school data,
   role-scoped and cited. The gateway's first resident; every later feature
   reuses its retrieval + authorization layer.
2. **Fee-payment risk scoring + smart reminders** (#162, P1) — the money
   feature; multilingual (EN/Kiswahili) reminder drafts, human-approved bulk
   send, attribution recorded so ROI is provable.
3. **Report-card narratives** (#161, P1) — teacher-editable term comments
   generated from real records; the clearest teacher time-saver.
4. **Principal's weekly briefing** (#166, P2) — scheduled operations digest
   over approved aggregates, delivered by the notification fabric.
5. **Early-warning signals** (#163, P2) — chronic absenteeism and
   performance-decline detection with guardian-friendly explanations.
6. **Document-intelligence onboarding** (#164, P2) — OCR intake into review
   queues; bulk migration of paper records.
7. **AI grading assist** (#165, P2) — rubric-based first-pass marking,
   teacher approves every grade.
8. **Parent AI concierge on WhatsApp** (#167, P2-P3) — guardian-scoped
   answers on the channel parents already use; needs guardian identity
   linking groundwork.

Supporting foundations from the main roadmap: event consumer framework
(#171) + notifications (#172) unlock the communication-dependent features;
analytics read models (#180) provide the citation layer.

## Constitutional constraints (ADR-010 — non-negotiable)

- Intelligence **recommends**; deterministic domains decide; authorized
  humans approve. AI never writes to domain tables, never approves payments,
  never modifies the ledger.
- AI answers flow through the **same authorization and tenant-isolation
  layers** as human users — a teacher's assistant cannot see what a teacher
  cannot see.
- **Cite-or-withhold**: every displayed fact links to underlying records;
  unmapped content is visibly labeled provisional.
- Prompt/response audit records for sensitive operations.
- School data is **never used to train third-party models by default**;
  provider contracts must guarantee zero retention (or equivalent), and the
  gateway stays provider-swappable per deployment (demo vs production vs
  on-premise school groups).

## Positioning note

Generic competitors bolt a chatbot onto a form app. Skolara's intelligence
sits *behind* a ledger-grade, tenant-isolated, audit-logged domain core —
the AI is constrained by the same constitution that keeps school money and
records correct. That is the investor story: **not "we added AI", but "our
data foundation makes AI answers trustworthy where it matters — children,
money, and attendance."**
