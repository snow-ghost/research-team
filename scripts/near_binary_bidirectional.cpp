#include <algorithm>
#include <cstdint>
#include <iostream>
#include <limits>
#include <vector>

using State = uint64_t;

static State encode(const std::vector<int>& a) {
    State result = 0;
    for (int value : a) result = (result << 4) | State(value);
    return result;
}
static std::vector<int> decode(State value, int n) {
    std::vector<int> a(n);
    for (int i = n - 1; i >= 0; --i) { a[i] = int(value & 15); value >>= 4; }
    return a;
}
static uint64_t mix(uint64_t x) {
    x += 0x9e3779b97f4a7c15ULL;
    x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9ULL;
    x = (x ^ (x >> 27)) * 0x94d049bb133111ebULL;
    return x ^ (x >> 31);
}
static State step(State value, int n, int i, bool inverse) {
    int positions[4];
    State block[4];
    for (int j = 0; j < 4; ++j) {
        positions[j] = 4 * (n - 1 - (i + j) % n);
        block[j] = (value >> positions[j]) & 15;
    }
    for (int j = 0; j < 4; ++j) {
        value &= ~(State(15) << positions[j]);
        value |= block[(j + (inverse ? 3 : 1)) % 4] << positions[j];
    }
    return value;
}
struct Index {
    std::vector<State> keys;
    std::vector<uint32_t> values;
    size_t mask;
    explicit Index(size_t capacity) : keys(capacity), values(capacity, std::numeric_limits<uint32_t>::max()), mask(capacity - 1) {}
    size_t slot(State key) const {
        size_t s = mix(key) & mask;
        while (values[s] != std::numeric_limits<uint32_t>::max() && keys[s] != key) s = (s + 1) & mask;
        return s;
    }
    bool contains(State key) const { return values[slot(key)] != std::numeric_limits<uint32_t>::max(); }
    uint32_t get(State key) const { return values[slot(key)]; }
    void insert(State key, uint32_t value) {
        size_t s = slot(key);
        keys[s] = key;
        values[s] = value;
    }
};
struct Side {
    Index index;
    std::vector<State> states;
    std::vector<uint8_t> dist;
    std::vector<uint32_t> parent;
    std::vector<int16_t> via;
    size_t begin = 0;
    explicit Side(size_t capacity) : index(capacity) {}
    void add(State state) {
        index.insert(state, 0);
        states.push_back(state);
        dist.push_back(0);
        parent.push_back(0);
        via.push_back(-1);
    }
    bool extend(int depth, const Side& other, uint32_t& meeting) {
        size_t end = states.size();
        for (uint32_t q = begin; q < end; ++q) {
            for (int i = 0; i < 16; ++i) for (int inv = 0; inv < 2; ++inv) {
                State next = step(states[q], 16, i, inv);
                if (index.contains(next)) continue;
                uint32_t id = states.size();
                index.insert(next, id);
                states.push_back(next);
                dist.push_back(dist[q] + 1);
                parent.push_back(q);
                via.push_back(int16_t(2 * i + inv));
                if (other.index.contains(next)) {
                    meeting = id;
                    return true;
                }
            }
        }
        begin = end;
        std::cerr << "depth=" << depth << " layer=" << (states.size() - begin) << " total=" << states.size() << "\n";
        return false;
    }
};
static std::vector<int> pathFromRoot(const Side& side, uint32_t id) {
    std::vector<int> path;
    while (id != 0) {
        path.push_back(side.via[id]);
        id = side.parent[id];
    }
    std::reverse(path.begin(), path.end());
    return path;
}
int main(int argc, char** argv) {
    if (argc != 2) return 2;
    int unique = std::atoi(argv[1]);
    if (unique < 1 || unique > 8) return 2;
    Side forward(size_t(1) << 26), backward(size_t(1) << 26);
    std::vector<int> initial(16);
    for (int i = 0; i < unique; ++i) initial[i] = i;
    for (int i = unique; i < 16; ++i) initial[i] = unique;
    std::vector<int> target = initial;
    std::reverse(target.begin(), target.end());
    forward.add(encode(initial));
    backward.add(encode(target));
    for (int depth = 1; depth <= 7; ++depth) {
        uint32_t unused = 0;
        if (forward.extend(depth, backward, unused)) return 3;
    }
    for (int depth = 1; depth <= 9; ++depth) {
        uint32_t meeting = 0;
        if (backward.extend(depth, forward, meeting)) {
            State meetingState = backward.states[meeting];
            std::vector<int> left = pathFromRoot(forward, forward.index.get(meetingState));
            std::vector<int> backwardPath = pathFromRoot(backward, meeting);
            std::vector<int> right;
            for (auto it = backwardPath.rbegin(); it != backwardPath.rend(); ++it) right.push_back(*it ^ 1);
            std::vector<int> word = left;
            word.insert(word.end(), right.begin(), right.end());
            State verified = encode(initial);
            for (int op : word) verified = step(verified, 16, op / 2, op % 2);
            if (verified != encode(target)) return 4;
            std::cout << "left=" << left.size() << " right=" << right.size() << "\n";
            std::cout << "distance=" << word.size() << "\n";
            std::cout << "no_path_through=" << (7 + depth - 1) << "\n";
            std::cout << "state=";
            for (int value : decode(meetingState, 16)) std::cout << value;
            std::cout << "\nword=";
            for (int op : word) std::cout << (op % 2 ? 'R' : 'L') << op / 2 << ' ';
            std::cout << "\nverified=true\n";
            return 0;
        }
    }
    std::cout << "no_meeting_within_16\n";
    return 1;
}
