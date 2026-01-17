"""GibRAM protobuf protocol encoder/decoder."""

import struct
from typing import Tuple, Optional, Any, Dict
from enum import IntEnum
from .proto import gibram_pb2 as pb
from .exceptions import ProtocolError


class CodecType(IntEnum):
    """Wire protocol codec types."""

    PROTOBUF = 0x01  # Must match server expectation


class _Protocol:
    """
    Protobuf protocol encoder/decoder for GibRAM.
    
    Wire format: [codec: 1 byte][length: 4 bytes BE][protobuf envelope]
    """

    PROTOCOL_VERSION = 1
    CODEC = CodecType.PROTOBUF
    HEADER_SIZE = 5  # 1 byte codec + 4 bytes length

    @staticmethod
    def encode_envelope(
        cmd_type: int, payload: bytes = b"", request_id: int = 0, session_id: str = ""
    ) -> bytes:
        """
        Encode request envelope.

        Args:
            cmd_type: Command type from pb.CommandType
            payload: Serialized protobuf payload
            request_id: Request ID for tracking
            session_id: Session identifier

        Returns:
            Framed bytes ready to send
        """
        envelope = pb.Envelope(
            version=_Protocol.PROTOCOL_VERSION,
            request_id=request_id,
            cmd_type=cmd_type,
            payload=payload,
            session_id=session_id,
        )
        envelope_bytes = envelope.SerializeToString()

        # Frame: [codec][length][envelope]
        frame = struct.pack(">BI", _Protocol.CODEC, len(envelope_bytes)) + envelope_bytes
        return frame

    @staticmethod
    def decode_envelope(data: bytes) -> Tuple[int, int, bytes]:
        """
        Decode response envelope.

        Args:
            data: Raw bytes from server

        Returns:
            Tuple of (cmd_type, request_id, payload_bytes)

        Raises:
            ProtocolError: If data is malformed or codec mismatch
        """
        if len(data) < _Protocol.HEADER_SIZE:
            raise ProtocolError(f"Response too short: {len(data)} bytes")

        codec, length = struct.unpack(">BI", data[: _Protocol.HEADER_SIZE])

        if codec != _Protocol.CODEC:
            raise ProtocolError(f"Unsupported codec: {codec}, expected {_Protocol.CODEC}")

        if len(data) < _Protocol.HEADER_SIZE + length:
            raise ProtocolError(
                f"Incomplete envelope: expected {length} bytes, got {len(data) - _Protocol.HEADER_SIZE}"
            )

        envelope_bytes = data[_Protocol.HEADER_SIZE : _Protocol.HEADER_SIZE + length]
        envelope = pb.Envelope()

        try:
            envelope.ParseFromString(envelope_bytes)
        except Exception as e:
            raise ProtocolError(f"Failed to parse envelope: {e}") from e

        return envelope.cmd_type, envelope.request_id, envelope.payload

    @staticmethod
    def encode_add_document(external_id: str, filename: str = "") -> bytes:
        """Encode ADD_DOCUMENT request."""
        req = pb.AddDocumentRequest(external_id=external_id, filename=filename)
        return req.SerializeToString()

    @staticmethod
    def decode_document(payload: bytes) -> Dict[str, Any]:
        """Decode Document response."""
        doc = pb.Document()
        doc.ParseFromString(payload)
        return {
            "id": doc.id,
            "external_id": doc.external_id,
            "filename": doc.filename,
            "status": doc.status,
        }

    @staticmethod
    def encode_add_text_unit(
        external_id: str,
        document_id: int,
        content: str,
        embedding: list,
        token_count: int,
    ) -> bytes:
        """Encode ADD_TEXTUNIT request."""
        req = pb.AddTextUnitRequest(
            external_id=external_id,
            document_id=document_id,
            content=content,
            embedding=embedding,
            token_count=token_count,
        )
        return req.SerializeToString()

    @staticmethod
    def decode_text_unit(payload: bytes) -> Dict[str, Any]:
        """Decode TextUnit response."""
        tu = pb.TextUnit()
        tu.ParseFromString(payload)
        return {
            "id": tu.id,
            "external_id": tu.external_id,
            "document_id": tu.document_id,
            "content": tu.content,
            "token_count": tu.token_count,
        }

    @staticmethod
    def encode_add_entity(
        external_id: str, title: str, entity_type: str, description: str, embedding: list
    ) -> bytes:
        """Encode ADD_ENTITY request."""
        req = pb.AddEntityRequest(
            external_id=external_id,
            title=title,
            type=entity_type,
            description=description,
            embedding=embedding,
        )
        return req.SerializeToString()

    @staticmethod
    def decode_entity(payload: bytes) -> Dict[str, Any]:
        """Decode Entity response."""
        ent = pb.Entity()
        ent.ParseFromString(payload)
        return {
            "id": ent.id,
            "external_id": ent.external_id,
            "title": ent.title,
            "type": ent.type,
            "description": ent.description,
        }

    @staticmethod
    def encode_add_relationship(
        external_id: str,
        source_id: int,
        target_id: int,
        rel_type: str,
        description: str,
        weight: float,
    ) -> bytes:
        """Encode ADD_RELATIONSHIP request."""
        req = pb.AddRelationshipRequest(
            external_id=external_id,
            source_id=source_id,
            target_id=target_id,
            type=rel_type,
            description=description,
            weight=weight,
        )
        return req.SerializeToString()

    @staticmethod
    def decode_relationship(payload: bytes) -> Dict[str, Any]:
        """Decode Relationship response."""
        rel = pb.Relationship()
        rel.ParseFromString(payload)
        return {
            "id": rel.id,
            "external_id": rel.external_id,
            "source_id": rel.source_id,
            "target_id": rel.target_id,
            "type": rel.type,
            "description": rel.description,
            "weight": rel.weight,
        }

    @staticmethod
    def encode_link_text_unit_entity(textunit_id: int, entity_id: int) -> bytes:
        """Encode LINK_TEXTUNIT_ENTITY request."""
        req = pb.LinkTextUnitEntityRequest(textunit_id=textunit_id, entity_id=entity_id)
        return req.SerializeToString()

    @staticmethod
    def encode_query(query_vector: list, search_types: list, top_k: int) -> bytes:
        """Encode QUERY request."""
        req = pb.QueryRequest(
            query_vector=query_vector, search_types=search_types, top_k=top_k
        )
        return req.SerializeToString()

    @staticmethod
    def decode_query_response(payload: bytes) -> Dict[str, Any]:
        """Decode QueryResponse."""
        resp = pb.QueryResponse()
        resp.ParseFromString(payload)

        entities = []
        for ent_result in resp.entities:
            entities.append(
                {
                    "entity": {
                        "id": ent_result.entity.id,
                        "title": ent_result.entity.title,
                        "type": ent_result.entity.type,
                        "description": ent_result.entity.description,
                    },
                    "similarity": ent_result.similarity,
                    "hop": ent_result.hop,
                }
            )

        text_units = []
        for tu_result in resp.textunits:
            text_units.append(
                {
                    "textunit": {
                        "id": tu_result.textunit.id,
                        "content": tu_result.textunit.content,
                        "document_id": tu_result.textunit.document_id,
                    },
                    "similarity": tu_result.similarity,
                    "hop": tu_result.hop,
                }
            )

        communities = []
        for comm_result in resp.communities:
            communities.append(
                {
                    "community": {
                        "id": comm_result.community.id,
                        "title": comm_result.community.title,
                        "summary": comm_result.community.summary,
                        "entity_ids": list(comm_result.community.entity_ids),
                    },
                    "similarity": comm_result.similarity,
                }
            )

        return {
            "entities": entities,
            "textunits": text_units,
            "communities": communities,
            "execution_time_ms": resp.stats.duration_micros / 1000.0 if resp.stats else 0.0,
        }

    @staticmethod
    def encode_compute_communities(resolution: float) -> bytes:
        """Encode COMPUTE_COMMUNITIES request."""
        req = pb.ComputeCommunitiesRequest(resolution=resolution)
        return req.SerializeToString()

    @staticmethod
    def decode_compute_communities_response(payload: bytes) -> int:
        """Decode ComputeCommunitiesResponse."""
        resp = pb.ComputeCommunitiesResponse()
        resp.ParseFromString(payload)
        return resp.count

    @staticmethod
    def decode_info_response(payload: bytes) -> Dict[str, Any]:
        """Decode InfoResponse."""
        resp = pb.InfoResponse()
        resp.ParseFromString(payload)
        return {
            "version": resp.version,
            "document_count": resp.document_count,
            "textunit_count": resp.textunit_count,
            "entity_count": resp.entity_count,
            "relationship_count": resp.relationship_count,
            "community_count": resp.community_count,
            "vector_dim": resp.vector_dim,
            "session_count": resp.session_count,
        }
