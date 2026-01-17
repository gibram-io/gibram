"""Low-level GibRAM protocol client."""

import struct
from typing import List, Dict, Any, Optional
from .proto import gibram_pb2 as pb
from ._connection import _Connection
from ._protocol import _Protocol
from .exceptions import ServerError, ProtocolError


class _Client:
    """
    Low-level GibRAM protocol client.
    
    Internal use only - handles protobuf communication with server.
    """

    def __init__(
        self,
        host: str,
        port: int,
        session_id: str,
        timeout: float = 30.0,
    ):
        self.host = host
        self.port = port
        self.session_id = session_id
        self.timeout = timeout
        self._conn: Optional[_Connection] = None
        self._request_id = 0

    def connect(self):
        """Establish connection to server."""
        self._conn = _Connection(self.host, self.port, self.timeout)
        self._conn.connect()

    def _execute(self, cmd_type: int, payload: bytes = b"") -> bytes:
        """
        Execute command and return response payload.

        Args:
            cmd_type: Command type from pb.CommandType
            payload: Serialized request payload

        Returns:
            Response payload bytes

        Raises:
            ServerError: If server returns error
            ProtocolError: If response is malformed
        """
        if not self._conn:
            self.connect()

        # Encode request
        self._request_id += 1
        request = _Protocol.encode_envelope(cmd_type, payload, self._request_id, self.session_id)

        # Send request
        self._conn.send(request)

        # Read response header to get payload length
        header = self._conn.recv(_Protocol.HEADER_SIZE)
        codec, length = struct.unpack(">BI", header)
        
        if codec != _Protocol.CODEC:
            raise ProtocolError(f"Unsupported codec: {codec}, expected {_Protocol.CODEC}")

        # Read full response
        full_response = header + self._conn.recv(length)
        resp_cmd, resp_req_id, resp_payload = _Protocol.decode_envelope(full_response)

        # Check for error response
        if resp_cmd == pb.CommandType.CMD_ERROR:
            error = pb.Error()
            error.ParseFromString(resp_payload)
            raise ServerError(f"Server error: {error.message} (code: {error.code})")

        return resp_payload

    def ping(self) -> bool:
        """Health check."""
        try:
            self._execute(pb.CommandType.CMD_PING, b"")
            return True
        except Exception:
            return False

    def get_server_info(self) -> Dict[str, Any]:
        """Get server statistics."""
        payload = self._execute(pb.CommandType.CMD_INFO, b"")
        return _Protocol.decode_info_response(payload)

    def add_document(self, external_id: str, filename: str = "") -> int:
        """
        Add document.

        Args:
            external_id: External document identifier
            filename: Original filename

        Returns:
            Document ID
        """
        req_payload = _Protocol.encode_add_document(external_id, filename)
        resp_payload = self._execute(pb.CommandType.CMD_ADD_DOCUMENT, req_payload)
        doc = _Protocol.decode_document(resp_payload)
        return doc["id"]

    def add_text_unit(
        self,
        external_id: str,
        document_id: int,
        content: str,
        embedding: List[float],
        token_count: int,
    ) -> int:
        """
        Add text unit.

        Args:
            external_id: External identifier
            document_id: Parent document ID
            content: Text content
            embedding: Embedding vector
            token_count: Token count

        Returns:
            TextUnit ID
        """
        req_payload = _Protocol.encode_add_text_unit(
            external_id, document_id, content, embedding, token_count
        )
        resp_payload = self._execute(pb.CommandType.CMD_ADD_TEXTUNIT, req_payload)
        tu = _Protocol.decode_text_unit(resp_payload)
        return tu["id"]

    def add_entity(
        self,
        external_id: str,
        title: str,
        entity_type: str,
        description: str,
        embedding: List[float],
    ) -> int:
        """
        Add entity.

        Args:
            external_id: External identifier
            title: Entity title
            entity_type: Entity type (person, organization, concept, etc)
            description: Entity description
            embedding: Embedding vector

        Returns:
            Entity ID
        """
        req_payload = _Protocol.encode_add_entity(
            external_id, title, entity_type, description, embedding
        )
        resp_payload = self._execute(pb.CommandType.CMD_ADD_ENTITY, req_payload)
        entity = _Protocol.decode_entity(resp_payload)
        return entity["id"]

    def add_relationship(
        self,
        external_id: str,
        source_id: int,
        target_id: int,
        rel_type: str,
        description: str,
        weight: float,
    ) -> int:
        """
        Add relationship.

        Args:
            external_id: External identifier
            source_id: Source entity ID
            target_id: Target entity ID
            rel_type: Relationship type
            description: Relationship description
            weight: Relationship weight (0.0 to 1.0)

        Returns:
            Relationship ID
        """
        req_payload = _Protocol.encode_add_relationship(
            external_id, source_id, target_id, rel_type, description, weight
        )
        resp_payload = self._execute(pb.CommandType.CMD_ADD_RELATIONSHIP, req_payload)
        rel = _Protocol.decode_relationship(resp_payload)
        return rel["id"]

    def link_text_unit_entity(self, textunit_id: int, entity_id: int):
        """
        Link text unit to entity.

        Args:
            textunit_id: TextUnit ID
            entity_id: Entity ID
        """
        req_payload = _Protocol.encode_link_text_unit_entity(textunit_id, entity_id)
        self._execute(pb.CommandType.CMD_LINK_TEXTUNIT_ENTITY, req_payload)

    def query(
        self,
        query_vector: List[float],
        search_types: List[str],
        top_k: int,
    ) -> Dict[str, Any]:
        """
        Execute vector query.

        Args:
            query_vector: Query embedding vector
            search_types: Types to search (["entity", "textunit", "community"])
            top_k: Number of results

        Returns:
            Query results with entities, textunits, communities
        """
        req_payload = _Protocol.encode_query(query_vector, search_types, top_k)
        resp_payload = self._execute(pb.CommandType.CMD_QUERY, req_payload)
        return _Protocol.decode_query_response(resp_payload)

    def compute_communities(self, resolution: float = 1.0) -> int:
        """
        Run community detection.

        Args:
            resolution: Leiden algorithm resolution parameter

        Returns:
            Number of communities detected
        """
        req_payload = _Protocol.encode_compute_communities(resolution)
        resp_payload = self._execute(pb.CommandType.CMD_COMPUTE_COMMUNITIES, req_payload)
        return _Protocol.decode_compute_communities_response(resp_payload)

    def close(self):
        """Close connection."""
        if self._conn:
            self._conn.close()
            self._conn = None

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
