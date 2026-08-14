"""The fixed BrowseComp-Plus corpus and the two tools the agent searches it with.

BrowseComp-Plus replaces live web search with 100,195 documents, so a query
retrieves the same documents on every run. That is what makes an energy
comparison between two knob settings mean something: the agent does the same
work unless a knob changes it.

The agent gets `search` for BM25 over the corpus and `open` for reading one
document a page at a time. Both are the tools the benchmark's own harness
gives an agent.

The BM25 index builds in about a minute and is saved under `index/`, so only
the first run pays for it.

Environment: INDEX_DIR, INDEX_DOC_CHARS, CORPUS_DATASET.
"""

from __future__ import annotations

import os
import re
from dataclasses import dataclass
from pathlib import Path

import bm25s
from datasets import Dataset, load_dataset

CORPUS_DATASET = os.environ.get("CORPUS_DATASET", "Tevatron/browsecomp-plus-corpus")
INDEX_DIR = Path(os.environ.get("INDEX_DIR", "index"))

# How much of each document BM25 sees. The median document is 9,800 characters
# and the longest is 3.2 million, so a cap keeps one outlier from dominating
# the index. Reading through `open` is uncapped.
INDEX_DOC_CHARS = int(os.environ.get("INDEX_DOC_CHARS", "16000"))

SNIPPET_CHARS = 320


@dataclass(frozen=True)
class Hit:
    """One search result. The snippet is a window around the first query term
    the document mentions."""

    docid: str
    url: str
    snippet: str


class Corpus:
    """BM25 search and paged reads over the BrowseComp-Plus corpus. Holds no
    per-query state, so one instance is safe to share across threads."""

    def __init__(self, index_dir: Path = INDEX_DIR) -> None:
        self._docs = load_dataset(CORPUS_DATASET, split="train")
        self._docids: list[str] = self._docs["docid"]
        self._row = {docid: i for i, docid in enumerate(self._docids)}
        self._retriever = _load_or_build(self._docs, index_dir)

    def search(self, query: str, k: int) -> list[Hit]:
        tokens = bm25s.tokenize(query, stopwords="en", show_progress=False)
        rows, _ = self._retriever.retrieve(tokens, k=min(k, len(self._docids)), show_progress=False)
        return [self._hit(int(row), query) for row in rows[0]]

    def open(self, docid: str, page: int, page_chars: int) -> tuple[str, int]:
        """One page of a document and how many pages it has. An unknown docid
        raises KeyError so the agent sees the failure as a tool error."""
        text = self._docs[self._row[docid]]["text"]
        pages = max(1, -(-len(text) // page_chars))
        page = max(1, min(page, pages))
        return text[(page - 1) * page_chars : page * page_chars], pages

    def _hit(self, row: int, query: str) -> Hit:
        doc = self._docs[row]
        return Hit(docid=doc["docid"], url=doc["url"], snippet=_snippet(doc["text"], query))


def _snippet(text: str, query: str) -> str:
    terms = [t for t in re.findall(r"\w+", query.lower()) if len(t) > 3]
    lowered = text[:INDEX_DOC_CHARS].lower()
    starts = [pos for pos in (lowered.find(t) for t in terms) if pos >= 0]
    start = max(0, min(starts) - 80) if starts else 0
    return " ".join(text[start : start + SNIPPET_CHARS].split())


def _load_or_build(docs: Dataset, index_dir: Path) -> bm25s.BM25:
    if index_dir.exists():
        return bm25s.BM25.load(str(index_dir), load_corpus=False)

    print(f"building the BM25 index over {len(docs):,} documents, about a minute", flush=True)
    # Each document is capped as its row decodes, so memory stays at the capped
    # size. The whole text column is 3.2 GB.
    capped = [row["text"][:INDEX_DOC_CHARS] for row in docs.select_columns(["text"])]
    tokens = bm25s.tokenize(capped, stopwords="en", show_progress=False)
    retriever = bm25s.BM25()
    retriever.index(tokens, show_progress=False)
    retriever.save(str(index_dir))
    print(f"index saved to {index_dir}", flush=True)
    return retriever
