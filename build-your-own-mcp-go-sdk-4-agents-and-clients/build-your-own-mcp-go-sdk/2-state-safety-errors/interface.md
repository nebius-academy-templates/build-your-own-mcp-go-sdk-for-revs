# Interface worksheet: state

**Related**:

> [CONTRACT](CONTRACT.md): The state the server keeps, to compare your answers with afterwards
> [README](README.md): Task 1 asks for this worksheet

Decide which answers are worth keeping on the server between calls, before you build any of it. Fill this in before you read `CONTRACT.md`, sections 3 and 6. Afterwards, compare your answers with the contract and note where you differ and why. Nothing here is graded.

## Links
**Internal**:

- `2-state-safety-errors/CONTRACT.md`
- `1-surface-and-contract/interface.md`

## How to fill it in
| Column | Write |
|---|---|
| Costly to make | `yes` if making the answer reads many files or many lines, `no` otherwise |
| Asked for again | `yes` if a later call is likely to need the same answer, or part of it |
| Handle | `yes` if the server should keep the answer and return an id for it, `no` otherwise |
| Why | One line |

## Which answers earn a handle
| # | Answer | Costly to make | Asked for again | Handle | Why |
|---|---|---|---|---|---|
| 1 | The files under a folder and their formats (`list_logs`) | | | | |
| 2 | Counts and message groups for a folder over a day (`summarize_logs`) | | | | |
| 3 | The lines that match a pattern (`search_logs`) | | | | |
| 4 | The lines around one line (`read_lines`) | | | | |
| 5 | The formats the server reads (`triage://formats`) | | | | |
| 6 | The findings saved so far (`triage://reports`) | | | | |

## The handle
For the answers you marked `yes`:

| Question | Your answer |
|---|---|
| What does an id look like, and why can't a caller guess another one? | |
| How long does an id work, and who decides? | |
| Who may use an id? | |
| How many ids does one caller hold, and what happens to the oldest? | |
| What does the server keep for an id, and what does it read again from the files? | |
| What does a caller see when its id is gone, and what should it do next? | |

## Your notes
Where your design differs from `CONTRACT.md`, and why:

- 
