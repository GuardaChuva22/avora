#version 440
// Orb: a polished chrome sphere lit by a slowly orbiting light, with a
// specular sheen, a cold rim and a breathing halo. "level" (0..1) lifts the
// glow while Avora is thinking.

layout(location = 0) in vec2 qt_TexCoord0;
layout(location = 0) out vec4 fragColor;

layout(std140, binding = 0) uniform buf {
    mat4 qt_Matrix;
    float qt_Opacity;
    float time;
    float level;
};

void main()
{
    vec2 p = qt_TexCoord0 * 2.0 - 1.0;
    float r = length(p);
    const float R = 0.6;

    // Sphere.
    vec3 sphere = vec3(0.0);
    float a = 0.0;
    if (r < R + 0.01) {
        vec2 q = p / R;
        float z = sqrt(max(0.0, 1.0 - dot(q, q)));
        vec3 n = normalize(vec3(q, z));
        float t = time * (0.35 + level * 0.9);
        vec3 L = normalize(vec3(-0.45 + 0.35 * cos(t), -0.75 + 0.12 * sin(t * 1.3), 0.85));
        float diff = pow(max(dot(n, L), 0.0), 1.5);
        float spec = pow(max(dot(reflect(-L, n), vec3(0.0, 0.0, 1.0)), 0.0), 60.0);
        float rim = pow(1.0 - z, 2.6);
        // Environment band: a horizon line reflected on the chrome.
        float band = smoothstep(0.22, 0.0, abs(n.y - 0.22 - 0.05 * sin(t * 0.7))) * 0.07 * z;
        vec3 c = vec3(0.045, 0.045, 0.055)
               + vec3(0.80, 0.81, 0.85) * diff
               + vec3(1.0) * spec * 0.85
               + vec3(0.62, 0.66, 0.78) * rim * 0.55
               + vec3(band);
        a = smoothstep(R + 0.006, R - 0.006, r);
        sphere = c;
    }

    // Halo outside the sphere.
    float breathe = 0.82 + 0.18 * sin(time * 1.4);
    float h = exp(-pow(max(r - R, 0.0) * 5.2, 2.0)) * (0.20 + level * 0.30) * breathe;
    h *= smoothstep(1.0, 0.8, r);

    vec3 col = sphere * a + vec3(0.92, 0.94, 1.0) * h * (1.0 - a);
    float alpha = a + h * (1.0 - a);
    fragColor = vec4(col, alpha) * qt_Opacity;
}
