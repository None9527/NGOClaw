from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class GenerateRequest(_message.Message):
    __slots__ = ("prompt", "model", "provider", "max_tokens", "temperature", "top_p", "metadata", "history", "system_instruction")
    class MetadataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    TOP_P_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    HISTORY_FIELD_NUMBER: _ClassVar[int]
    SYSTEM_INSTRUCTION_FIELD_NUMBER: _ClassVar[int]
    prompt: str
    model: str
    provider: str
    max_tokens: int
    temperature: float
    top_p: float
    metadata: _containers.ScalarMap[str, str]
    history: _containers.RepeatedCompositeFieldContainer[ChatMessage]
    system_instruction: str
    def __init__(self, prompt: _Optional[str] = ..., model: _Optional[str] = ..., provider: _Optional[str] = ..., max_tokens: _Optional[int] = ..., temperature: _Optional[float] = ..., top_p: _Optional[float] = ..., metadata: _Optional[_Mapping[str, str]] = ..., history: _Optional[_Iterable[_Union[ChatMessage, _Mapping]]] = ..., system_instruction: _Optional[str] = ...) -> None: ...

class ChatMessage(_message.Message):
    __slots__ = ("role", "content", "name")
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    role: str
    content: str
    name: str
    def __init__(self, role: _Optional[str] = ..., content: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class GenerateResponse(_message.Message):
    __slots__ = ("request_id", "content", "model_used", "tokens_used", "finish_reason", "metadata")
    class MetadataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_USED_FIELD_NUMBER: _ClassVar[int]
    TOKENS_USED_FIELD_NUMBER: _ClassVar[int]
    FINISH_REASON_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    content: str
    model_used: str
    tokens_used: int
    finish_reason: str
    metadata: _containers.ScalarMap[str, str]
    def __init__(self, request_id: _Optional[str] = ..., content: _Optional[str] = ..., model_used: _Optional[str] = ..., tokens_used: _Optional[int] = ..., finish_reason: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ...) -> None: ...

class GenerateStreamChunk(_message.Message):
    __slots__ = ("request_id", "content", "is_final")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    IS_FINAL_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    content: str
    is_final: bool
    def __init__(self, request_id: _Optional[str] = ..., content: _Optional[str] = ..., is_final: bool = ...) -> None: ...

class ImageRequest(_message.Message):
    __slots__ = ("prompt", "model", "width", "height", "num_images")
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    NUM_IMAGES_FIELD_NUMBER: _ClassVar[int]
    prompt: str
    model: str
    width: int
    height: int
    num_images: int
    def __init__(self, prompt: _Optional[str] = ..., model: _Optional[str] = ..., width: _Optional[int] = ..., height: _Optional[int] = ..., num_images: _Optional[int] = ...) -> None: ...

class ImageResponse(_message.Message):
    __slots__ = ("image_urls", "model_used")
    IMAGE_URLS_FIELD_NUMBER: _ClassVar[int]
    MODEL_USED_FIELD_NUMBER: _ClassVar[int]
    image_urls: _containers.RepeatedScalarFieldContainer[str]
    model_used: str
    def __init__(self, image_urls: _Optional[_Iterable[str]] = ..., model_used: _Optional[str] = ...) -> None: ...

class SkillRequest(_message.Message):
    __slots__ = ("skill_id", "input", "config")
    class ConfigEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SKILL_ID_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    skill_id: str
    input: str
    config: _containers.ScalarMap[str, str]
    def __init__(self, skill_id: _Optional[str] = ..., input: _Optional[str] = ..., config: _Optional[_Mapping[str, str]] = ...) -> None: ...

class SkillResponse(_message.Message):
    __slots__ = ("output", "success", "error_message")
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    ERROR_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    output: str
    success: bool
    error_message: str
    def __init__(self, output: _Optional[str] = ..., success: bool = ..., error_message: _Optional[str] = ...) -> None: ...

class HealthCheckRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HealthCheckResponse(_message.Message):
    __slots__ = ("status", "version", "providers_status")
    class Status(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        UNKNOWN: _ClassVar[HealthCheckResponse.Status]
        SERVING: _ClassVar[HealthCheckResponse.Status]
        NOT_SERVING: _ClassVar[HealthCheckResponse.Status]
    UNKNOWN: HealthCheckResponse.Status
    SERVING: HealthCheckResponse.Status
    NOT_SERVING: HealthCheckResponse.Status
    class ProvidersStatusEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: bool
        def __init__(self, key: _Optional[str] = ..., value: bool = ...) -> None: ...
    STATUS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    PROVIDERS_STATUS_FIELD_NUMBER: _ClassVar[int]
    status: HealthCheckResponse.Status
    version: str
    providers_status: _containers.ScalarMap[str, bool]
    def __init__(self, status: _Optional[_Union[HealthCheckResponse.Status, str]] = ..., version: _Optional[str] = ..., providers_status: _Optional[_Mapping[str, bool]] = ...) -> None: ...
