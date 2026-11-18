#version 440
// Backdrop: near-black base, a cold white bloom from the top, a faint second
// light low on the right and a gentle vignette. No grain: it read as dirt.
// Rendered once into a texture (Backdrop.qml), so it costs nothing per frame.

layout(location = 0) in vec2 qt_TexCoord0;
layout(location = 0) out vec4 fragColor;

layout(std140, binding = 0) uniform buf {
    mat4 qt_Matrix;
    float qt_Opacity;
    vec2 size;
};

void main()
{
    vec2 uv = qt_TexCoord0;
    float aspect = size.x / max(size.y, 1.0);
    vec3 col = vec3(0.018, 0.018, 0.022);

    // Top bloom, wide and soft.
    vec2 d1 = (uv - vec2(0.56, -0.18)) * vec2(aspect * 0.55, 1.0);
    col += vec3(0.93, 0.94, 1.0) * exp(-dot(d1, d1) * 3.4) * 0.11;

    // Second, dimmer light low on the right.
    vec2 d2 = (uv - vec2(1.02, 1.08)) * vec2(aspect * 0.8, 1.0);
    col += vec3(0.85, 0.88, 1.0) * exp(-dot(d2, d2) * 5.0) * 0.045;

    // Vignette.
    float v = smoothstep(1.25, 0.35, length((uv - 0.5) * vec2(1.0, 1.15)));
    col *= mix(0.80, 1.0, v);

    // Ordered dither of under one 8-bit step: hides banding in the bloom
    // without any visible texture.
    vec2 px = floor(uv * size);
    col += (fract(dot(px, vec2(0.7548777, 0.5698403))) - 0.5) / 255.0;

    fragColor = vec4(col, 1.0) * qt_Opacity;
}
