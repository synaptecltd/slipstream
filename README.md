# Design principles

1. The protocol is designed for streaming raw measurement data, similar to the IEC 61850-9-2 Sampled Value protocol. It is designed to support high sample rate continuous point on wave (CPOW) voltage and current data, and also supports other measurement types.
2. The protocol should be flexible. It should support any number of samples per message so that it can be applied for different applications. For example, 2 to 8 samples per message may be appropriate for real-time applications while still benefitting from compression, and much larger messages could be used for archiving fault or event records, with excellent compression opportunities.
3. Optimise for the lowest data size for minimal network data transfer and for small file sizes for saving event captures to permanent storage.
4. Assume that out-of-band communications will agree sampling rate and number of variables to be transferred, similar to IEEE C37.118.2 configuration frames and STTP. This helps reduce the amount of data to be send in the main data stream to allow successful decoding.
5. Ensure that data quality and time synchronisation information are strictly preserved and provided for every data sample.
6. Prefer efficient encode and decode processing, with up-front allocations, where possible. However, the compression will naturally improve the end-to-end latency, due to the reduced data to be processed and transferred.

7. It should produce a byte stream which is suitable for a variety of transport methods, such as Ethernet, UDP, TCP, HTTP, and WebSocket.
8. An error in one message should only invalidate that message, and not future messages.
9. General purpose compression algorithms have already been shown to be ineffective or computationally expensive for CPOW data [^1]

# Data types

32-bit signed integer for values. This could be extended for floating-point values in the future, using the method in [^2]

32-bit unsigned integer for quality. This is intended to be based on the IEC 61850 quality specification.

64-bit signed integer for timestamp. This is based on the Go language representation, using nanoseconds relative to 1st January 1970 UTC, which is limited to a date between the years 1678 and 2262. Timestamps in STTP are restricted to 100 ns resolution, while suitable for output values such as frequency, it is very inaccurate for CPOW data, which could be sampled at inconvenient rates such as 14.4 kHz (so the 69444 ns sampling period would be truncated to 69400.00 ns, leading to an intrinsic 44.44 ns error). IEC 61850 timestamps

// TODO IEC time quality

# Protocol details

It is assumed that every sample is included for the duration of the message. If a sample was missed (e.g. due to the sensor or underlying data source being unavailable), a zero sample should be added and the data quality should be adjusted appropriately. This simplifies the encoding and significantly reduces the amount of data to be sent because only the starting timestamp needs to be included per message, and all other timestamps can be inferred. Therefore, a single 64-bit field can encode the timestamp, rather than 64 bits per sample.

Wherever possible, variable length encoding is used (with zig-zag encoding for signed values, the same as Google Protocol Buffers).

The first sample must be encoded in full. The second sample is encoded as the difference from the first sample (delta encoding). All remaining samples are encoded using delta-delta encoding.

The quality is assumed to not change very often. Therefore, it is encoded using run-length encoding (RLE). A special run-length of `0` is used to represent that all future values within the same message are the same. So, for the common case where the quality value is `0` for all samples, that can be encoded in one byte for the value and one byte for the number of samples.

There are four sections of the protocol:

1. Header
2. First sample data encoding
3. Second and later sample encoding
4. Quality values for each sample

The protocol header contains the following fields:

1. UUID, 16 bytes
2. Timestamp of the first sample

The next thing to encode is the first sample of each variable. Then, each sample is encoded using delta or delta-delta encoding. After all samples are encoded, the quality RLE section is encoded.

# Other

The protocol compression tends to perform better for higher sampling rates, because the difference between samples is less and, on average, fewer bytes are required. Similarly, the protocol compression tends to perform worse for larger RMS values of voltage and current because the differences between samples is greater.

A disadvantage of the protocol is that changes in data values or quality values will increase the message size. This means that more data must be send or recorded when important or interesting events occur, compared with the steady-state.

Decoders must have knowledge of the encoding parameters. This means that Wireshark may be unable to provide diagnostic information, unless it is also able to access and decode the out-of-band data which describes the protocol instance (i.e. the sampling rate and number of variables).

[^1]: M. Anderson paper
[^2]: Compression paper
