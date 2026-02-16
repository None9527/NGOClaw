"""JSON-RPC 2.0 Sideload Handler

Dispatches incoming JSON-RPC requests to the appropriate handler
(provider/generate, tool/execute, hook/invoke).
"""

import asyncio
import json
import logging
import sys
from typing import Callable, Dict, Any, Optional

logger = logging.getLogger(__name__)


class SideloadHandler:
    """JSON-RPC 2.0 request dispatcher for sideload mode.

    Reads line-delimited JSON from stdin, dispatches to registered
    method handlers, and writes responses to stdout.
    """

    def __init__(self):
        self._methods: Dict[str, Callable] = {}
        self._running = False

    def register_method(self, name: str, handler: Callable) -> None:
        """Register a method handler (async function)."""
        self._methods[name] = handler

    async def handle_request(self, raw: str) -> Optional[str]:
        """Parse and dispatch a single JSON-RPC request.

        Returns JSON response string, or None for notifications.
        """
        try:
            msg = json.loads(raw)
        except json.JSONDecodeError as e:
            return json.dumps({
                "jsonrpc": "2.0", "id": None,
                "error": {"code": -32700, "message": f"Parse error: {e}"}
            })

        method = msg.get("method", "")
        params = msg.get("params", {})
        msg_id = msg.get("id")

        handler = self._methods.get(method)
        if handler is None:
            if msg_id is None:
                return None  # notification for unknown method — ignore
            return json.dumps({
                "jsonrpc": "2.0", "id": msg_id,
                "error": {"code": -32601, "message": f"Method not found: {method}"}
            })

        try:
            result = await handler(params)
            if msg_id is None:
                return None  # notification — no response
            return json.dumps({
                "jsonrpc": "2.0", "id": msg_id,
                "result": result
            })
        except Exception as e:
            logger.exception(f"Error handling {method}")
            if msg_id is None:
                return None
            return json.dumps({
                "jsonrpc": "2.0", "id": msg_id,
                "error": {"code": -32603, "message": str(e)}
            })

    async def send_notification(self, method: str, params: dict) -> None:
        """Send a JSON-RPC notification (no id) to stdout."""
        line = json.dumps({
            "jsonrpc": "2.0",
            "method": method,
            "params": params
        })
        sys.stdout.write(line + "\n")
        sys.stdout.flush()

    async def run(self) -> None:
        """Main loop: read stdin line by line, dispatch, write to stdout."""
        self._running = True
        logger.info("Sideload handler started, reading from stdin")

        loop = asyncio.get_event_loop()
        reader = asyncio.StreamReader()
        protocol = asyncio.StreamReaderProtocol(reader)
        await loop.connect_read_pipe(lambda: protocol, sys.stdin)

        while self._running:
            try:
                line = await reader.readline()
                if not line:
                    logger.info("EOF on stdin, shutting down")
                    break

                raw = line.decode("utf-8").strip()
                if not raw:
                    continue

                response = await self.handle_request(raw)
                if response is not None:
                    sys.stdout.write(response + "\n")
                    sys.stdout.flush()

            except asyncio.CancelledError:
                break
            except Exception:
                logger.exception("Error in sideload read loop")

        self._running = False
        logger.info("Sideload handler stopped")

    def stop(self) -> None:
        """Signal the handler to stop."""
        self._running = False
