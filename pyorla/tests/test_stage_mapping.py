"""Tests for pyorla.stage_mapping."""

import pytest

from pyorla.stage import Stage
from pyorla.stage_mapping import (
    ExplicitStageMapping,
    StageMappingInput,
    apply_stage_mapping_output,
)
from pyorla.types import LLMBackend


def _backend(name: str = "be") -> LLMBackend:
    return LLMBackend(name=name, endpoint="http://x", model_id="m1")


def test_explicit_mapping_valid():
    b = _backend()
    s1 = Stage("stage-a", b)
    s2 = Stage("stage-b", b)
    mapping = ExplicitStageMapping()
    output = mapping.map(StageMappingInput(stages=[s1, s2], backends=[b]))
    assert s1.id in output.assignments
    assert s2.id in output.assignments


def test_explicit_mapping_no_backend_raises():
    s1 = Stage("stage-a")
    mapping = ExplicitStageMapping()
    with pytest.raises(ValueError, match="no backend"):
        mapping.map(StageMappingInput(stages=[s1], backends=[]))


def test_apply_stage_mapping_output():
    b1 = _backend("b1")
    b2 = _backend("b2")
    s1 = Stage("stage-a", b1)
    s1.set_max_tokens(100)

    mapping = ExplicitStageMapping()
    output = mapping.map(StageMappingInput(stages=[s1], backends=[b1]))
    output.assignments[s1.id].backend = b2
    output.assignments[s1.id].max_tokens = 200

    apply_stage_mapping_output([s1], output)
    assert s1.backend is not None
    assert s1.backend.name == "b2"
    assert s1.max_tokens == 200
