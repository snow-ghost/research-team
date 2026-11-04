#include <algorithm>
#include <array>
#include <iostream>
#include <vector>
#include <numeric>

constexpr int N = 10;
constexpr int F = 7;
using Features = std::array<int, F>;
using Weights = std::array<int, F>;

int inversions(const std::vector<int>& a) {
    std::vector<int> order;
    for (int v : a) if (v < 6) order.push_back(v);
    int result = 0;
    for (int i = 0; i < 6; ++i) for (int j = i + 1; j < 6; ++j) {
        if ((5 - order[i]) > (5 - order[j])) ++result;
    }
    return result;
}
int cd(int a, int b) {
    int d = std::abs(a - b);
    return std::min(d, N - d);
}
Features features(const std::vector<int>& a) {
    const int target[N] = {6,6,6,5,4,3,2,1,0,6};
    int circular = 0, linear = 0, square = 0, cap2 = 0, cap3 = 0, cap4 = 0;
    for (int v = 0; v < 6; ++v) {
        int p = int(std::find(a.begin(), a.end(), v) - a.begin());
        int d = cd(p, target[v]);
        circular += d;
        square += d * d;
        int dl = std::abs(p - target[v]);
        linear += dl;
        cap2 += std::min(dl, 2);
        cap3 += std::min(dl, 3);
        cap4 += std::min(dl, 4);
    }
    return {circular, inversions(a), cap2, cap3, cap4, square, linear};
}
std::vector<int> step(const std::vector<int>& a, int i, bool inverse) {
    auto b = a;
    int block[4];
    for (int j = 0; j < 4; ++j) block[j] = a[(i + j) % N];
    for (int j = 0; j < 4; ++j) b[(i + j) % N] = block[(j + (inverse ? 3 : 1)) % 4];
    return b;
}
int dot(const Features& a, const Weights& w) {
    int result = 0;
    for (int i = 0; i < F; ++i) result += a[i] * w[i];
    return result;
}
bool valid(const Weights& w) {
    std::vector<int> positions{0,1,2,3,4,5,6,7,8,9};
    do {
        std::vector<int> values{0,1,2,3,4,5};
        do {
            std::vector<int> a(N, 6);
            for (int i = 0; i < 6; ++i) a[positions[i]] = values[i];
            auto old = features(a);
            for (int i = 0; i < N; ++i) for (int inv = 0; inv < 2; ++inv) {
                if (dot(old, w) - dot(features(step(a, i, inv)), w) > 6) return false;
            }
        } while (std::next_permutation(values.begin(), values.end()));
    } while (std::next_permutation(positions.begin(), positions.end()));
    return true;
}
int main() {
    std::vector<int> initial(N, 6);
    std::iota(initial.begin(), initial.begin() + 6, 0);
    auto start = features(initial);
    std::vector<Weights> candidates;
    for (int w0 = 0; w0 <= 6; ++w0) for (int w1 = 0; w1 <= 6; ++w1)
    for (int w2 = 0; w2 <= 6; ++w2) for (int w3 = 0; w3 <= 6; ++w3)
    for (int w4 = 0; w4 <= 6; ++w4) for (int w5 = 0; w5 <= 6; ++w5)
    for (int w6 = 0; w6 <= 6; ++w6) {
        Weights w{w0,w1,w2,w3,w4,w5,w6};
        if (dot(start, w) == 48) candidates.push_back(w);
    }
    std::cerr << "candidates=" << candidates.size() << "\n";
    for (const auto& w : candidates) {
        if (valid(w)) {
            std::cout << "candidate weights=" << w[0] << "," << w[1] << "," << w[2] << "," << w[3] << "," << w[4] << "," << w[5] << "," << w[6] << "\n";
            return 1;
        }
    }
    return 0;
}
