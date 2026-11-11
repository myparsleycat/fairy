These fixtures were generated from a 97 x 65 RGB pattern:

```python
rgb = ((x * 17 + y * 3) % 256,
       (x * 5 + y * 13) % 256,
       (x * 7 + y * 11) % 256)
alpha = (x * 9 + y * 5) % 256
```

- `progressive.jpg`: RGB pattern encoded with `cjpeg -quality 90 -progressive`.
- `lossy.webp`: RGB pattern encoded with FFmpeg's `libwebp -lossless 0 -quality 80`.
- `lossless.webp`: RGB pattern encoded with `libwebp -lossless 1 -quality 80`.
- `alpha.webp`: RGBA pattern encoded with `libwebp -lossless 0 -quality 80`.

The tests read the committed fixtures and do not require these encoders.
