# Slipstream

Slipstream is a method for lossless compression of power system data.

## Example usage

Open the [example file](https://github.com/synaptecltd/slipstream/example/example.go) and run `go run example.go`. Typical operation is summarised below.

### Initialise an encoder

```Go
// define settings
uuid := uuid.New()
variablePerSample := 8   // number of "variables", such as voltages or currents. 8 is equivalent to IEC 61850-9-2 LE
samplingRate := 4800     // Hz
samplesPerMessage := 480 // each message contains 100 ms of data

// initialise an encoder
enc := slipstream.NewEncoder(uuid, variablePerSample, samplingRate, samplesPerMessage)
```

The encoder can be reused for subsequent messages.

### Generate test data samples

```Go
// use the Synaptec "emulator" library to generate three-phase voltage and current test signals
emulator := &emulator.Emulator{
    SamplingRate: samplingRate,
    Fnom:         50.0,
    Ts:           1 / float64(samplingRate),
    I: &emulator.ThreePhaseEmulation{
        PosSeqMag: 500.0,
    },
    V: &emulator.ThreePhaseEmulation{
        PosSeqMag: 400000.0 / math.Sqrt(3) * math.Sqrt(2),
    },
}

// use emulator to generate test data
samplesToEncode := 480 // equates to 1 full message
data := createInputData(emulator, samplesToEncode, variablePerSample)
```

### Encode data using Slipstream

```Go
// loop through data samples and encode into Slipstream format
for d := range data {
    buf, length, err := enc.Encode(&data[d])

    // check if message encoding has finished
    if err == nil && length > 0 {
        // buf should now contain an encoded message, and can be send over the network or stored
    }
}
```

### Initialise and use a decoder

```Go
// initialise a decoder
dec := slipstream.NewDecoder(uuid, variablePerSample, samplingRate, samplesPerMessage)

// decode the message
errDecode := dec.DecodeToBuffer(buf, length)

// iterate through the decoded samples
if errDecode == nil {
    var decodedData []float64 = make([]float64, samplesToEncode)
    for i := range dec.Out {
        // extract individual values
        for j := 0; j < dec.Int32Count; j++ {
            fmt.Println("timestamp:", dec.Out[i].T, "value:", dec.Out[i].Int32s[j], "quality:", dec.Out[i].Q[j])
        }
    }
}
```

<!-- ### Optionally, use features to optimise the encoding efficiency

Call this before encoding:

```
``` -->

## Design principles

1. The protocol is designed for streaming raw measurement data, similar to the IEC 61850-9-2 Sampled Value protocol. It is designed to support high sample rate continuous point on wave (CPOW) voltage and current data, and also supports other measurement types.
2. The data compression must be lossless.
3. The protocol should be flexible. It should support any number of samples per message so that it can be used for different applications where efficient representation of measurement data is useful. For example, 2 to 8 samples per message may be appropriate for real-time applications while still benefitting from compression, and much larger messages could be used for archiving fault or event records, with excellent compression opportunities.
4. Optimise for the lowest data size for minimal network data transfer and for small file sizes for saving event captures to permanent storage.
5. Assume that out-of-band communications will agree sampling rate and number of variables to be transferred, similar to IEEE C37.118.2 configuration frames and STTP. This helps reduce the amount of data to be sent in the main data stream to allow successful decoding.
6. Ensure that data quality and time synchronisation information are strictly preserved and provided for every data sample.
7. Prefer efficient encode and decode processing, with up-front allocations, where possible. However, the compression will naturally improve the end-to-end latency, due to the reduced data to be processed and transferred.
8. The protocol should produce a byte stream which is suitable for a variety of transport methods, such as Ethernet, UDP, TCP, HTTP, WebSocket, and saving to a file.
9. An error in one message should only invalidate that message, and not future messages.
10. General purpose compression algorithms have already been shown to be ineffective or computationally expensive for CPOW data [^1].

## Data types

* 32-bit signed integer for all data values. This requires a scaled integer representation for floating-point data, but this approach has already been adopted for IEC 61850-9-2 encoding. However, the protocol could be extended for directly representing floating-point values in the future, using the method in [^2].
* 32-bit unsigned integer for quality. This is intended to be based on the IEC 61850 quality specification, for which only 14 bits are used (including the "derived" indicator), and only 16 bits should ever be used. It is proposed here that the most significant byte is used for time quality, with the two least significant bytes used for data quality according to the IEC 61850 approach. The exact use is not prescribed at present, but 32 bits per data sample has been provisioned.
* 64-bit signed integer for timestamp. This is based on the Go language representation, using nanoseconds relative to 1st January 1970 UTC, which is limited to a date between the years 1678 and 2262. Timestamps in STTP are restricted to 100 ns resolution, while suitable for output values such as synchrophasors and frequency, it is very inaccurate for CPOW data, which could be sampled at inconvenient rates such as 14.4 kHz (so the 69444 ns sampling period would be truncated to 69400.00 ns, leading to an intrinsic 44.44 ns error). If the start of the data capture was always aligned to the second roll over point then the fraction of second value would always be zero, but the protocol should not be restricted in this way. Similarly, IEC 61850 and IEEE C37.118.2 timestamps only dedicate 24 bits to the fraction of second and have a poor resolution limit of 59.6 ns.

## Protocol details

With knowledge of the underlying data, the compression method can be tailored for much greater performance than generic compression such as gzip [^3]. Each quantity is compressed and encoded separately. This is the same method used in [^1], and is similar to how TimescaleDB performs compression on columns of data over time, rather than generically compressing each row as they are added to the database.

It is assumed that every sample is included for the defined message size. If a sample was missed (e.g. due to the sensor or underlying data source being unavailable), a zero sample should be added and the data quality should be adjusted appropriately. This simplifies the encoding and significantly reduces the amount of data to be sent because only the starting timestamp needs to be included per message, and all other timestamps can be inferred. Therefore, a single 64-bit field can encode the timestamp, rather than 64 bits per sample.

Wherever possible, variable length encoding is used (with zig-zag encoding for signed values, the same as Google Protocol Buffers).

The first sample must be encoded in full. The second sample is encoded as the difference from the first sample (delta encoding). All remaining samples are encoded using delta-delta encoding, and the number of "layers" of the delta-delta encoding can be configured. If a relatively large number of values is included per message (such as for an event record), simple-8b encoding can be used to improve the packing of the variable-length integer values. It is slightly better to use simple-8b for all values, even the first and second values.

The quality is assumed to not change very often. Therefore, it is encoded using run-length encoding (RLE). A special run-length of `0` is used to represent that all future values within the same message are the same. So, for the common case where the quality value is `0` for all samples, that can be encoded in one byte for the value plus one byte for the number of samples.

There are four sections of each message using the protocol:

1. Header
2. First sample data encoding
3. Second and later sample encoding
4. Quality values for each sample

The protocol header contains the following fields:

1. UUID, 16 bytes
2. Timestamp of the first sample, 8 bytes
3. Number of encoded samples, variable length

The next thing to encode is the first sample of each variable. Then, each sample is encoded using delta or delta-delta encoding. After all samples are encoded, the quality RLE section is encoded.

## Compression performance

Compression performance can typically reduce data to about 15% of the theoretical uncompressed sample size (assuming 4 bytes for data, 4 bytes for quality, and 8 bytes for timestamp). Higher sampling rates can compress down to <5%, often requiring less than 1 byte per new sample on average. Shorter messages with fewer samples will achieve compression of 15-25%. Compared to IEC 61850-9-2 SV, the performance is even better due to the additional overhead and repeated data inherent in the SV ASDU structure. For example, sampling at 14.4 kHz with 6 samples (ASDUs) per message (using the "LE" dataset with 8 variables) requires about 589 bytes for SV (including Ethernet header, but not including the "RefrTm" timestamp). This new protocol only requires about 134 bytes to convey the same information.

The protocol compression tends to perform better for higher sampling rates, because the difference between samples is less and, on average, fewer bytes are required. Similarly, the protocol compression tends to perform worse for larger RMS values of voltage and current because the differences between samples is greater.

A disadvantage of the protocol is that changes in data values or quality values will increase the message size. This means that more data must be send or recorded when important or interesting events occur, compared with the steady-state.

Random noise in the encoded quantities will reduce compression performance. Harmonics will also have this effect, but to a lesser extent.

## Other notes

Decoders must have knowledge of the encoding parameters. This means that Wireshark may be unable to provide diagnostic information, unless it is also able to access and decode the out-of-band data which describes the protocol instance (i.e. the sampling rate and number of variables).

It is not possible to decode the quality values until all the data values in a message are decoded first.

It is assumed that three-phase quantities should also include a neutral component, similar to the IEC 61850 "LE" profile.

The IEC 61850-9-2 SV protocol allows some flexibility. For example, in principle, ASDUs in the same message could be mixed from different datasets. This new protocol does not allow this. However, it would be simpler modify the SV dataset to encompass the additional data, rather than having multiple datasets, or send separate SV streams. It would make also make it less complex to encode and decode, compared to mixing ASDU from different datasets.

Internally, the encoder uses an alternating ping-pong buffer. This means that it is acceptable to read the output of the encoder while a new message starts being encoded. However, the output from the first message must be fully saved or copied before a third message is started. The encoder is not thread-safe, so a single instance should only be used from the same thread. This to ensure that the order of calls to Encode() is preserved. While mutex locking will synchronise access, it does not queue subsequent calls to Encode().

## Tests

You can run the test suite locally with:

```
go test -v
```

## References

[^1]: Blair, S. M., Roscoe, A. J., & Irvine, J. (2016). Real-time compression of IEC 61869-9 sampled value data. 2016 IEEE International Workshop on Applied Measurements for Power Systems (AMPS), 1–6. https://doi.org/10.1109/AMPS.2016.7602854 https://strathprints.strath.ac.uk/57710/1/Blair_etal_AMPS2016_Real_time_compression_of_IEC_61869_9_sampled_value_data.pdf
[^2]: Andersen, M. P., & Culler, D. (2016). BTrDB: Optimizing Storage System Design for Timeseries Processing. 14th USENIX Conference on File and Storage Technologies (FAST ’16). https://www.usenix.org/conference/fast16/technical-sessions/presentation/andersen
[^3]: https://arxiv.org/pdf/1209.2137.pdf
