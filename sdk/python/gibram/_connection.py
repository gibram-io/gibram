"""Low-level TCP connection handling for GibRAM protocol."""

import socket
import struct
from typing import Optional
from .exceptions import ConnectionError, TimeoutError


class _Connection:
    """
    TCP connection wrapper for GibRAM server.
    
    Internal use only - handles socket I/O.
    """

    def __init__(self, host: str, port: int, timeout: float = 30.0):
        self.host = host
        self.port = port
        self.timeout = timeout
        self._socket: Optional[socket.socket] = None

    def connect(self):
        """Establish TCP connection to server."""
        try:
            self._socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self._socket.settimeout(self.timeout)
            self._socket.connect((self.host, self.port))
        except socket.timeout as e:
            raise TimeoutError(f"Connection timeout to {self.host}:{self.port}") from e
        except OSError as e:
            raise ConnectionError(
                f"Failed to connect to {self.host}:{self.port}: {e}"
            ) from e

    def send(self, data: bytes):
        """Send data to server."""
        if not self._socket:
            raise ConnectionError("Not connected")

        try:
            self._socket.sendall(data)
        except socket.timeout as e:
            raise TimeoutError("Send timeout") from e
        except OSError as e:
            raise ConnectionError(f"Send failed: {e}") from e

    def recv(self, size: int) -> bytes:
        """Receive exactly size bytes from server."""
        if not self._socket:
            raise ConnectionError("Not connected")

        data = b""
        while len(data) < size:
            try:
                chunk = self._socket.recv(size - len(data))
                if not chunk:
                    raise ConnectionError("Connection closed by server")
                data += chunk
            except socket.timeout as e:
                raise TimeoutError("Receive timeout") from e
            except OSError as e:
                raise ConnectionError(f"Receive failed: {e}") from e

        return data

    def close(self):
        """Close connection."""
        if self._socket:
            try:
                self._socket.close()
            except OSError:
                pass
            finally:
                self._socket = None

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
