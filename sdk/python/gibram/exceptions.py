"""GibRAM exception hierarchy."""


class GibRAMError(Exception):
    """Base exception for all GibRAM errors."""

    pass


class ConnectionError(GibRAMError):
    """Connection-related errors."""

    pass


class TimeoutError(GibRAMError):
    """Operation timeout errors."""

    pass


class ProtocolError(GibRAMError):
    """Protocol encoding/decoding errors."""

    pass


class ServerError(GibRAMError):
    """Server-side errors."""

    pass


class NotFoundError(GibRAMError):
    """Resource not found errors."""

    pass


class ValidationError(GibRAMError):
    """Input validation errors."""

    pass


class ExtractionError(GibRAMError):
    """Entity extraction errors."""

    pass


class EmbeddingError(GibRAMError):
    """Embedding generation errors."""

    pass


class ConfigurationError(GibRAMError):
    """Configuration errors (missing API keys, invalid params, etc)."""

    pass
