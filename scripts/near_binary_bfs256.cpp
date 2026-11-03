#include <algorithm>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <limits>
#include <vector>

struct State {
    uint64_t word[4] = {0, 0, 0, 0};
};

static uint64_t mix(uint64_t x) {
    x += 0x9e3779b97f4a7c15ULL;
    x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9ULL;
    x = (x ^ (x >> 27)) * 0x94d049bb133111ebULL;
    return x ^ (x >> 31);
}
static State encode(const std::vector<int>& a) {
    State x;
    for (int v : a) {
        uint64_t carry = uint64_t(v);
        for (int k = 0; k < 4; ++k) {
            uint64_t next = x.word[k] >> 60;
            x.word[k] = (x.word[k] << 4) | carry;
            carry = next;
        }
    }
    return x;
}
static uint64_t get(State x, int shift) {
    return (x.word[shift / 64] >> (shift % 64)) & 15;
}
static void set(State& x, int shift, uint64_t value) {
    x.word[shift / 64] &= ~(15ULL << (shift % 64));
    x.word[shift / 64] |= value << (shift % 64);
}
static std::vector<int> decode(State x, int n) {
    std::vector<int> a(n);
    for (int i = n - 1; i >= 0; --i) a[i] = int(get(x, 4 * i));
    return a;
}
static State step(State x, int n, int i, bool inverse) {
    int positions[4];
    uint64_t block[4];
    for (int j = 0; j < 4; ++j) {
        positions[j] = 4 * (n - 1 - (i + j) % n);
        block[j] = get(x, positions[j]);
    }
    for (int j = 0; j < 4; ++j) set(x, positions[j], block[(j + (inverse ? 3 : 1)) % 4]);
    return x;
}
static bool equal(State a, State b) {
    for (int k = 0; k < 4; ++k) if (a.word[k] != b.word[k]) return false;
    return true;
}
struct Index {
    std::vector<State> keys;
    std::vector<uint32_t> values;
    size_t mask;
    explicit Index(size_t capacity) : keys(capacity), values(capacity, std::numeric_limits<uint32_t>::max()), mask(capacity - 1) {}
    size_t slot(State key) const {
        size_t s = 0;
        for (int k = 0; k < 4; ++k) s = (s ^ mix(key.word[k])) & mask;
        while (values[s] != std::numeric_limits<uint32_t>::max() && !equal(keys[s], key)) s = (s + 1) & mask;
        return s;
    }
    bool contains(State key) const { return values[slot(key)] != std::numeric_limits<uint32_t>::max(); }
    void insert(State key, uint32_t value) {
        size_t s = slot(key);
        keys[s] = key;
        values[s] = value;
    }
};
int main(int argc, char** argv) {
    if (argc != 3) return 2;
    int n = std::atoi(argv[1]), copies = std::atoi(argv[2]);
    if (n < 5 || n > 64 || copies < 1 || copies >= n || n - copies > 15) return 2;
    size_t expected = 1, capacity = 1;
    for (int i = copies + 1; i <= n; ++i) expected *= i;
    while (capacity < expected * 2) capacity <<= 1;
    Index index(capacity);
    std::vector<int> initial(n);
    for (int i = 0; i < n - copies; ++i) initial[i] = i;
    for (int i = n - copies; i < n; ++i) initial[i] = n - copies;
    std::vector<State> states{encode(initial)};
    std::vector<uint8_t> dist{0};
    std::vector<uint32_t> parent{0};
    std::vector<int16_t> via{-1};
    states.reserve(expected);
    dist.reserve(expected);
    parent.reserve(expected);
    via.reserve(expected);
    index.insert(states[0], 0);
    size_t farthest = 0;
    for (uint32_t q = 0; q < states.size(); ++q) {
        if (q && q % 1000000 == 0) std::cerr << "processed=" << q << " discovered=" << states.size() << "\n";
        for (int i = 0; i < n; ++i) for (int inv = 0; inv < 2; ++inv) {
            State next = step(states[q], n, i, inv);
            if (index.contains(next)) continue;
            if (states.size() >= expected) return 3;
            uint32_t id = states.size();
            index.insert(next, id);
            states.push_back(next);
            dist.push_back(dist[q] + 1);
            parent.push_back(q);
            via.push_back(int16_t(2 * i + inv));
            if (dist.back() > dist[farthest]) farthest = id;
        }
    }
    std::cout << "n=" << n << " copies=" << copies << " states=" << states.size() << " eccentricity=" << int(dist[farthest]) << "\n";
    int shown = 0;
    for (uint32_t id = 0; id < states.size() && shown < 12; ++id) {
        if (dist[id] != dist[farthest]) continue;
        auto a = decode(states[id], n);
        std::cout << "state=";
        for (int v : a) std::cout << v;
        std::cout << " word=";
        std::vector<int> word;
        for (uint32_t p = id; p != 0; p = parent[p]) word.push_back(via[p]);
        std::reverse(word.begin(), word.end());
        for (int m : word) std::cout << (m & 1 ? "R" : "L") << (m / 2) << " ";
        std::cout << "\n";
        ++shown;
    }
    std::reverse(initial.begin(), initial.end());
    uint32_t flip = index.values[index.slot(encode(initial))];
    std::vector<int> flip_word;
    for (uint32_t p = flip; p != 0; p = parent[p]) flip_word.push_back(via[p]);
    std::reverse(flip_word.begin(), flip_word.end());
    std::cout << "flip_distance=" << int(dist[flip]) << " flip_word=";
    for (int m : flip_word) std::cout << (m & 1 ? "R" : "L") << (m / 2) << " ";
    std::cout << "\n";
}
