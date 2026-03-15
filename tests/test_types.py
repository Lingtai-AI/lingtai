from stoai.types import MCPTool, UnknownToolError


def test_mcp_tool_creation():
    tool = MCPTool(
        name="test_tool",
        schema={"type": "object", "properties": {"x": {"type": "integer"}}},
        description="A test tool",
        handler=lambda args: {"result": args["x"] * 2},
    )
    assert tool.name == "test_tool"
    assert tool.handler({"x": 5}) == {"result": 10}


def test_unknown_tool_error():
    err = UnknownToolError("bad_tool")
    assert "bad_tool" in str(err)
