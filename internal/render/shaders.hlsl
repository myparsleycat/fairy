// One instanced pipeline draws every primitive kind in painter's order.
// Rounded-rect, border and shadow math is adapted from Zed's GPUI (Apache-2.0);
// text alpha correction is adapted from Windows Terminal's dwrite.hlsl (MIT).

cbuffer Globals : register(b0) {
    float2 viewport;
    float2 atlas_size;
    float4 gamma_ratios;
    float grayscale_contrast;
    float3 globals_pad;
};

cbuffer Batch : register(b1) {
    uint batch_start;
    uint batch_pad;
    float2 origin; // the window pixel at the target's top-left, for offscreen layers
};

struct Prim {
    float4 bounds;       // x, y, w, h (device pixels)
    float4 clip;         // x, y, w, h
    float4 color;        // straight sRGB: fill, glyph or shadow color
    float4 color2;       // gradient end color
    float4 border_color;
    float4 radii;        // top-left, top-right, bottom-right, bottom-left
    float4 border;       // top, right, bottom, left
    float4 uv;           // texture rect: x, y, w, h (normalized)
    float4 params;       // shadow: x = sigma; gradient: x = angle in radians (0 = up)
    uint kind;
    uint flags;
    float2 xlate;        // FLAG_XFORM: translation of the affine map
    float4 xform;        // FLAG_XFORM: linear part (a, b, c, d): x' = a x + c y, y' = b x + d y
    float4 clip2;        // FLAG_XFORM: clip in the prim's own space, before the map
};

static const uint KIND_RECT = 0;
static const uint KIND_SHADOW = 1;
static const uint KIND_GLYPH = 2;
static const uint KIND_IMAGE = 3;
static const uint KIND_CALLOUT = 4;
static const uint KIND_ARC = 5;
static const uint KIND_WAVE = 6;
static const uint FLAG_GRADIENT = 1;
static const uint FLAG_INSET = 2;
static const uint FLAG_RAW_ALPHA = 4;
static const uint FLAG_PREMULTIPLIED = 8;
static const uint FLAG_XFORM = 16;
static const uint FLAG_BLUR = 32; // image prims: gaussian along params.xy (uv per tap), sigma params.z taps, taps clamped to color2 (u0, v0, u1, v1)
static const uint FLAG_DASHED = 64; // rect prims: dash the border
static const uint FLAG_PADDING_BOX = 128; // rect prims: fill stops inside the border
static const uint CLIP_SHIFT = 16; // flags >> CLIP_SHIFT: 1 + index into clips, or 0
static const float M_PI_F = 3.141592653;

// A rounded or feathered clip. Clips nest through parent.
struct ClipRec {
    float4 rect;    // x, y, w, h
    float4 radii;   // top-left, top-right, bottom-right, bottom-left
    float4 feather; // soft edge widths: top, right, bottom, left
    uint parent;    // 1 + index of the enclosing clip, or 0
    uint space;     // 0: window pixels; 1: the prim's own space, before FLAG_XFORM's map
    uint2 clip_pad;
};

StructuredBuffer<Prim> prims : register(t0);
Texture2D<float> atlas : register(t1);
Texture2D<float4> image : register(t2);
StructuredBuffer<ClipRec> clips : register(t3);
SamplerState samp : register(s0);

struct VOut {
    float4 pos : SV_Position;
    float2 uv : TEXCOORD0;
    nointerpolation uint id : TEXCOORD1;
    float2 local : TEXCOORD2; // the point in the prim's own space, where its shape is evaluated
    float2 screen : TEXCOORD3;
    float4 clip : SV_ClipDistance0;
    float4 clip2 : SV_ClipDistance1;
};

float4 clip_distances(float2 pos, float4 c) {
    return float4(pos.x - c.x, c.x + c.z - pos.x, pos.y - c.y, c.y + c.w - pos.y);
}

VOut vs_main(uint vid : SV_VertexID, uint iid : SV_InstanceID) {
    uint id = batch_start + iid;
    Prim p = prims[id];
    float2 unit = float2(float(vid & 1u), float((vid >> 1) & 1u));
    float4 b = p.bounds;
    if (p.kind == KIND_SHADOW) {
        if ((p.flags & FLAG_INSET) != 0) {
            // Shade the whole element, including the area outside the hole.
            b = p.uv;
        } else {
            float m = 3.0 * p.params.x;
            b.xy -= m;
            b.zw += 2.0 * m;
        }
    } else if (p.kind == KIND_CALLOUT) {
        float extent = p.params.z * sqrt(2.0) + 0.5;
        float2 lo = min(b.xy, p.params.xy - extent);
        float2 hi = max(b.xy + b.zw, p.params.xy + extent);
        b = float4(lo, hi - lo);
    }
    float2 pos = b.xy + unit * b.zw;
    float2 screen = pos;
    VOut o;
    o.clip2 = float4(1.0, 1.0, 1.0, 1.0);
    if ((p.flags & FLAG_XFORM) != 0) {
        screen = float2(p.xform.x * pos.x + p.xform.z * pos.y, p.xform.y * pos.x + p.xform.w * pos.y) + p.xlate;
        o.clip2 = clip_distances(pos, p.clip2);
    }
    o.pos = float4((screen - origin) / viewport * float2(2.0, -2.0) + float2(-1.0, 1.0), 0.0, 1.0);
    o.uv = p.uv.xy + unit * p.uv.zw;
    o.id = id;
    o.local = pos;
    o.screen = screen;
    o.clip = clip_distances(screen, p.clip);
    return o;
}

float pick_corner_radius(float2 center_to_point, float4 radii) {
    if (center_to_point.x < 0.0) {
        return center_to_point.y < 0.0 ? radii.x : radii.w;
    }
    return center_to_point.y < 0.0 ? radii.y : radii.z;
}

float quad_sdf_impl(float2 corner_center_to_point, float r) {
    if (r == 0.0) {
        return max(corner_center_to_point.x, corner_center_to_point.y);
    }
    float inset = length(max(float2(0.0, 0.0), corner_center_to_point)) +
        min(0.0, max(corner_center_to_point.x, corner_center_to_point.y));
    return inset - r;
}

float quad_sdf(float2 pt, float4 bounds, float4 radii) {
    float2 half_size = bounds.zw * 0.5;
    float2 ctp = pt - (bounds.xy + half_size);
    float r = pick_corner_radius(ctp, radii);
    return quad_sdf_impl(abs(ctp) - half_size + r, r);
}

float4 callout_color(Prim p, float2 pt) {
    float2 delta = pt - p.params.xy;
    float2 rotated = float2(delta.x + delta.y, delta.y - delta.x) * sqrt(0.5);
    float r = p.params.w;
    float diamond = quad_sdf_impl(abs(rotated) - p.params.z + r, r);
    float body = quad_sdf(pt, p.bounds, p.radii);
    // Fade the union once; blending two fills increases opacity at the overlap.
    return float4(p.color.rgb, p.color.a * saturate(0.5 - min(body, diamond)));
}

float quarter_ellipse_sdf(float2 pt, float2 radii) {
    float2 circle_vec = pt / radii;
    return (length(circle_vec) - 1.0) * (radii.x + radii.y) * -0.5;
}

float4 over(float4 below, float4 above) {
    float alpha = above.a + below.a * (1.0 - above.a);
    if (alpha <= 0.0) {
        return float4(0.0, 0.0, 0.0, 0.0);
    }
    float3 rgb = (above.rgb * above.a + below.rgb * below.a * (1.0 - above.a)) / alpha;
    return float4(rgb, alpha);
}

float4 background(Prim p, float2 pt) {
    if ((p.flags & FLAG_GRADIENT) == 0) {
        return p.color;
    }
    float angle = p.params.x - M_PI_F * 0.5;
    float2 dir = float2(cos(angle), sin(angle));
    float2 half_size = p.bounds.zw * 0.5;
    float2 ctp = pt - (p.bounds.xy + half_size);
    float extent = abs(dir.x) * half_size.x + abs(dir.y) * half_size.y;
    float t = saturate(dot(ctp, dir) / max(extent * 2.0, 0.0001) + 0.5);
    float4 a = float4(p.color.rgb * p.color.a, p.color.a);
    float4 b = float4(p.color2.rgb * p.color2.a, p.color2.a);
    float4 c = lerp(a, b, t);
    return c.a > 0.0 ? float4(c.rgb / c.a, c.a) : float4(0.0, 0.0, 0.0, 0.0);
}

float4 rect_color(Prim p, float2 pt) {
    float4 bg = background(p, pt);
    const float aa = 0.5;
    float2 half_size = p.bounds.zw * 0.5;
    float2 ctp = pt - (p.bounds.xy + half_size);
    float r = pick_corner_radius(ctp, p.radii);
    float2 corner_to_point = abs(ctp) - half_size;
    float2 ccp = corner_to_point + r;
    float outer_sdf = quad_sdf_impl(ccp, r);

    bool no_border = p.border.x == 0.0 && p.border.y == 0.0 && p.border.z == 0.0 && p.border.w == 0.0;
    if (no_border) {
        return float4(bg.rgb, bg.a * saturate(aa - outer_sdf));
    }

    float2 border = float2(ctp.x < 0.0 ? p.border.w : p.border.y, ctp.y < 0.0 ? p.border.x : p.border.z);
    float2 reduced = float2(border.x == 0.0 ? -aa : border.x, border.y == 0.0 ? -aa : border.y);
    float2 inner_corner_to_point = corner_to_point + reduced;

    float inner_sdf;
    if (ccp.x <= 0.0 || ccp.y <= 0.0) {
        inner_sdf = -max(inner_corner_to_point.x, inner_corner_to_point.y);
    } else if (inner_corner_to_point.x > 0.0 || inner_corner_to_point.y > 0.0) {
        inner_sdf = -1.0;
    } else if (reduced.x == reduced.y) {
        inner_sdf = -(outer_sdf + reduced.x);
    } else {
        float2 ellipse = max(float2(0.0, 0.0), float2(r, r) - reduced);
        inner_sdf = quarter_ellipse_sdf(ccp, ellipse);
    }

    // A dashed border keeps its corners solid and dashes each straight side as
    // CSS does: dashes three border widths long and two apart. The pattern
    // starts at the box's pixel-aligned edge so that dashes end on pixel edges.
    float dash = 1.0;
    if ((p.flags & FLAG_DASHED) != 0 && (ccp.x <= 0.0 || ccp.y <= 0.0)) {
        bool horizontal = corner_to_point.y > corner_to_point.x;
        float bw = max(horizontal ? border.y : border.x, 0.0001);
        float d = fmod(horizontal ? ctp.x + half_size.x : ctp.y + half_size.y, 5.0 * bw);
        float dist = d <= 3.0 * bw ? -min(d, 3.0 * bw - d) : min(d - 3.0 * bw, 5.0 * bw - d);
        dash = saturate(0.5 - dist);
    }

    float4 color = bg;
    float border_sdf = max(inner_sdf, outer_sdf);
    if (border_sdf < aa && dash > 0.0) {
        float4 blended = (p.flags & FLAG_PADDING_BOX) != 0 ? p.border_color : over(bg, p.border_color);
        // Mix premultiplied: a straight mix toward a transparent fill would
        // pull the edge's color toward black.
        float4 a = float4(bg.rgb * bg.a, bg.a);
        float4 b = float4(blended.rgb * blended.a, blended.a);
        float4 c = lerp(a, b, saturate(aa - inner_sdf) * dash);
        color = c.a > 0.0 ? float4(c.rgb / c.a, c.a) : float4(0.0, 0.0, 0.0, 0.0);
    }
    return float4(color.rgb, color.a * saturate(aa - outer_sdf));
}

float2 erf2(float2 x) {
    float2 s = sign(x);
    float2 a = abs(x);
    x = 1.0 + (0.278393 + (0.230389 + 0.078108 * (a * a)) * a) * a;
    x *= x;
    return s - s / (x * x);
}

float blur_along_x(float x, float y, float sigma, float corner, float2 half_size) {
    float delta = min(half_size.y - corner - abs(y), 0.0);
    float curved = half_size.x - corner + sqrt(max(0.0, corner * corner - delta * delta));
    float2 integral = 0.5 + 0.5 * erf2((x + float2(-curved, curved)) * (sqrt(0.5) / sigma));
    return integral.y - integral.x;
}

float gaussian(float x, float sigma) {
    return exp(-(x * x) / (2.0 * sigma * sigma)) / (sqrt(2.0 * M_PI_F) * sigma);
}

float4 shadow_color(Prim p, float2 pt) {
    float sigma = p.params.x;
    float2 half_size = p.bounds.zw * 0.5;
    float2 p0 = pt - (p.bounds.xy + half_size);
    float r = pick_corner_radius(p0, p.radii);
    float alpha;
    if (any(p.bounds.zw <= 0.0)) {
        alpha = 0.0;
    } else if (sigma <= 0.0) {
        alpha = saturate(0.5 - quad_sdf(pt, p.bounds, p.radii));
    } else {
        float low = p0.y - half_size.y;
        float high = p0.y + half_size.y;
        float start = clamp(-3.0 * sigma, low, high);
        float end = clamp(3.0 * sigma, low, high);
        float step = (end - start) / 4.0;
        float y = start + step * 0.5;
        alpha = 0.0;
        for (int i = 0; i < 4; i++) {
            alpha += blur_along_x(p0.x, p0.y - y, sigma, r, half_size) * gaussian(y, sigma) * step;
            y += step;
        }
    }
    if ((p.flags & FLAG_INSET) != 0) {
        // uv carries the element rect for inset shadows; border carries its radii.
        alpha = (1.0 - alpha) * saturate(0.5 - quad_sdf(pt, p.uv, p.border));
    }
    return float4(p.color.rgb, p.color.a * alpha);
}

// arc_color strokes a circular arc centered in bounds. params: x = start
// angle (radians, 0 at the top, clockwise), y = sweep, z = stroke width,
// w = 1 for round caps.
float4 arc_color(Prim p, float2 pt) {
    float2 c = p.bounds.xy + p.bounds.zw * 0.5;
    float2 d = pt - c;
    float half_w = p.params.z * 0.5;
    float radius = min(p.bounds.z, p.bounds.w) * 0.5 - half_w;
    float len = length(d);
    float ring = abs(len - radius) - half_w;
    float sweep = p.params.y;
    float dist = ring;
    if (sweep < 2.0 * M_PI_F) {
        float a = atan2(d.x, -d.y) - p.params.x;
        a = a - floor(a / (2.0 * M_PI_F)) * 2.0 * M_PI_F;
        if (a > sweep) {
            float a0 = p.params.x, a1 = p.params.x + sweep;
            float2 e0 = c + radius * float2(sin(a0), -cos(a0));
            float2 e1 = c + radius * float2(sin(a1), -cos(a1));
            if (p.params.w > 0.5) {
                dist = min(length(pt - e0), length(pt - e1)) - half_w;
            } else {
                float past = min(a - sweep, 2.0 * M_PI_F - a);
                dist = max(ring, past * max(len, 1.0));
            }
        }
    }
    return float4(p.color.rgb, p.color.a * saturate(0.5 - dist));
}

// wave_color draws a wavy line across bounds, as under a misspelled word.
// params: x = wavelength, y = line width.
float4 wave_color(Prim p, float2 pt) {
    float amp = max((p.bounds.w - p.params.y) * 0.5, 0.0);
    float k = 2.0 * M_PI_F / max(p.params.x, 1.0);
    float x = pt.x - p.bounds.x;
    float y = p.bounds.y + p.bounds.w * 0.5 + amp * sin(k * x);
    float slope = amp * k * cos(k * x);
    float dist = abs(pt.y - y) / sqrt(1.0 + slope * slope) - p.params.y * 0.5;
    return float4(p.color.rgb, p.color.a * saturate(0.5 - dist));
}

// clip_coverage is how much of the pixel the prim's rounded and feathered
// clips let through.
float clip_coverage(uint flags, float2 screen, float2 local) {
    uint c = flags >> CLIP_SHIFT;
    float cov = 1.0;
    [loop] for (int n = 0; n < 8 && c != 0; n++) {
        ClipRec r = clips[c - 1];
        float2 pt = r.space == 1 ? local : screen;
        cov *= saturate(0.5 - quad_sdf(pt, r.rect, r.radii));
        if (any(r.feather > 0.0)) {
            float4 edge = float4(pt.y - r.rect.y, r.rect.x + r.rect.z - pt.x, r.rect.y + r.rect.w - pt.y, pt.x - r.rect.x);
            float4 f = r.feather > 0.0 ? smoothstep(0.0, 1.0, saturate(edge / max(r.feather, 0.0001))) : 1.0;
            cov *= f.x * f.y * f.z * f.w;
        }
        c = r.parent;
    }
    return cov;
}

float color_brightness(float3 color) {
    return dot(color, float3(0.30, 0.59, 0.11));
}

float glyph_alpha(float sample, float3 color) {
    float brightness = color_brightness(color);
    float k = grayscale_contrast * saturate(4.0 * (0.75 - brightness));
    float contrasted = sample * (k + 1.0) / (sample * k + 1.0);
    float4 g = gamma_ratios;
    float adjust = g.x * brightness + g.y;
    float correction = adjust * contrasted + (g.z * brightness + g.w);
    return contrasted + contrasted * (1.0 - contrasted) * correction;
}

float4 ps_main(VOut i) : SV_Target {
    Prim p = prims[i.id];
    float2 pt = i.local;
    float cov = clip_coverage(p.flags, i.screen, i.local);
    float4 c;
    if (p.kind == KIND_RECT) {
        c = rect_color(p, pt);
    } else if (p.kind == KIND_CALLOUT) {
        c = callout_color(p, pt);
    } else if (p.kind == KIND_SHADOW) {
        c = shadow_color(p, pt);
    } else if (p.kind == KIND_ARC) {
        c = arc_color(p, pt);
    } else if (p.kind == KIND_WAVE) {
        c = wave_color(p, pt);
    } else if (p.kind == KIND_GLYPH) {
        float s = atlas.Sample(samp, i.uv);
        float a = (p.flags & FLAG_RAW_ALPHA) != 0 ? s : saturate(glyph_alpha(s, p.color.rgb));
        c = float4(p.color.rgb, p.color.a * a);
    } else if ((p.flags & FLAG_BLUR) != 0) {
        float sigma = max(p.params.z, 0.5);
        int n = min(int(ceil(3.0 * sigma)), 64);
        float4 sum = float4(0.0, 0.0, 0.0, 0.0);
        float wsum = 0.0;
        [loop] for (int k = -n; k <= n; k++) {
            float w = exp(-0.5 * float(k * k) / (sigma * sigma));
            sum += image.SampleLevel(samp, clamp(i.uv + p.params.xy * float(k), p.color2.xy, p.color2.zw), 0) * w;
            wsum += w;
        }
        return sum / wsum;
    } else {
        float4 s = image.Sample(samp, i.uv);
        float d = quad_sdf(pt, p.bounds, p.radii);
        float k = p.color.a * saturate(0.5 - d) * cov;
        if ((p.flags & FLAG_PREMULTIPLIED) != 0) {
            return s * k;
        }
        c = float4(s.rgb, s.a * k);
        return float4(c.rgb * c.a, c.a);
    }
    c.a *= cov;
    return float4(c.rgb * c.a, c.a);
}
